package legacydata

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/01121531/subandnew-api/common"
	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

const archiveVersion = 2

var protectedTables = map[string]struct{}{
	"authz_roles": {}, "casbin_rule": {}, "custom_oauth_providers": {},
	"managed_config_templates": {}, "managed_instance_alerts": {}, "managed_instance_audits": {},
	"managed_instance_config_bindings": {}, "managed_instance_credentials": {},
	"managed_instance_operation_batch_items": {}, "managed_instance_operation_batches": {},
	"managed_instance_operations": {}, "managed_instance_snapshots": {}, "managed_instances": {},
	"options": {}, "passkey_credentials": {}, "setups": {}, "system_instances": {},
	"system_task_locks": {}, "system_task_scope_locks": {}, "system_tasks": {},
	"two_fa_backup_codes": {}, "two_fas": {}, "user_oauth_bindings": {}, "users": {},
}

// This allowlist is intentionally explicit. Unknown extension tables must never
// become purge candidates merely because the control plane does not own them.
var knownLegacyTables = map[string]struct{}{
	"abilities": {}, "channels": {}, "checkins": {}, "dataset_capture_access_audit_items": {},
	"dataset_capture_access_audits": {}, "dataset_capture_indices": {}, "logs": {},
	"midjourneys": {}, "models": {}, "perf_metrics": {}, "prefill_groups": {},
	"proxies": {}, "proxy_group_waiters": {}, "proxy_groups": {}, "proxy_log_analyses": {},
	"proxy_log_analysis_cursors": {}, "proxy_state_events": {}, "proxy_upstream_attempts": {},
	"quota_data": {}, "redemptions": {}, "subscription_orders": {}, "subscription_plans": {},
	"subscription_pre_consume_records": {}, "tasks": {}, "tokens": {}, "top_ups": {},
	"user_subscriptions": {}, "vendors": {}, "channel_proxy_bindings": {},
}

var legacyUserColumns = []string{"access_token", "quota", "user_group"}

type Inventory struct {
	Version             int              `json:"version"`
	CreatedAt           string           `json:"created_at"`
	DatabaseDriver      string           `json:"database_driver"`
	DatabaseFingerprint string           `json:"database_fingerprint"`
	LegacyTables        []InventoryTable `json:"legacy_tables"`
	UnknownTables       []InventoryTable `json:"unknown_tables,omitempty"`
	LegacyUserColumns   []string         `json:"legacy_user_columns"`
	LegacyUserRowCount  int64            `json:"legacy_user_row_count,omitempty"`
	LegacyUserDataHash  string           `json:"legacy_user_data_hash,omitempty"`
}

type InventoryTable struct {
	Name        string          `json:"name"`
	RowCount    int64           `json:"row_count"`
	ContentHash string          `json:"content_hash"`
	Columns     []ArchiveColumn `json:"columns"`
}

type ArchiveColumn struct {
	Name         string `json:"name"`
	DatabaseType string `json:"database_type"`
	Nullable     bool   `json:"nullable"`
	PrimaryKey   bool   `json:"primary_key"`
}

type archiveHeader struct {
	Version             int               `json:"version"`
	CreatedAt           string            `json:"created_at"`
	DatabaseDriver      string            `json:"database_driver"`
	DatabaseFingerprint string            `json:"database_fingerprint"`
	LegacyUserColumns   []string          `json:"legacy_user_columns"`
	LegacyUserRowCount  int64             `json:"legacy_user_row_count,omitempty"`
	LegacyUserDataHash  string            `json:"legacy_user_data_hash,omitempty"`
	LegacyTables        []archiveTableRef `json:"legacy_tables"`
}

type archiveTableRef struct {
	Name        string          `json:"name"`
	RowCount    int64           `json:"row_count"`
	ContentHash string          `json:"content_hash"`
	Columns     []ArchiveColumn `json:"columns"`
}

func IsCommand(args []string) bool {
	return len(args) > 0 && args[0] == "legacy-data"
}

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "legacy-data" {
		fmt.Fprintln(stderr, "usage: subandnew-api legacy-data <inventory|archive|purge> [options]")
		return 2
	}
	db, _, err := model.OpenDatabaseWithoutMigration()
	if err != nil {
		fmt.Fprintln(stderr, "open database:", err)
		return 1
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}
	switch args[1] {
	case "inventory":
		return runInventory(db, args[2:], stdout, stderr)
	case "archive":
		return runArchive(db, args[2:], stdout, stderr)
	case "purge":
		return runPurge(db, args[2:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, "unknown legacy-data command:", args[1])
		return 2
	}
}

func runInventory(db *gorm.DB, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("legacy-data inventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "optional JSON output path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	inventory, err := BuildInventory(db)
	if err != nil {
		fmt.Fprintln(stderr, "inventory failed:", err)
		return 1
	}
	encoded, _ := json.MarshalIndent(inventory, "", "  ")
	encoded = append(encoded, '\n')
	if *output != "" {
		if err := os.WriteFile(*output, encoded, 0600); err != nil {
			fmt.Fprintln(stderr, "write inventory:", err)
			return 1
		}
	}
	_, _ = stdout.Write(encoded)
	return 0
}

func runArchive(db *gorm.DB, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("legacy-data archive", flag.ContinueOnError)
	flags.SetOutput(stderr)
	output := flags.String("output", "", "required archive JSON path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*output) == "" {
		fmt.Fprintln(stderr, "--output is required")
		return 2
	}
	inventory, err := BuildInventory(db)
	if err != nil {
		fmt.Fprintln(stderr, "inventory failed:", err)
		return 1
	}
	if len(inventory.UnknownTables) > 0 {
		fmt.Fprintln(stderr, "archive refused: unknown database tables require manual classification")
		printUnknownTables(stderr, inventory)
		return 1
	}
	checksum, err := WriteArchive(db, inventory, *output)
	if err != nil {
		fmt.Fprintln(stderr, "archive failed:", err)
		return 1
	}
	fmt.Fprintf(stdout, "archive=%s\nchecksum=%s\nfingerprint=%s\n", *output, checksum, inventory.DatabaseFingerprint)
	return 0
}

func runPurge(db *gorm.DB, args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("legacy-data purge", flag.ContinueOnError)
	flags.SetOutput(stderr)
	archivePath := flags.String("archive", "", "verified archive JSON path")
	execute := flags.Bool("execute", false, "perform destructive deletion")
	confirm := flags.String("confirm", "", "database fingerprint confirmation")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *archivePath == "" {
		fmt.Fprintln(stderr, "--archive is required")
		return 2
	}
	header, err := VerifyArchive(*archivePath)
	if err != nil {
		fmt.Fprintln(stderr, "archive verification failed:", err)
		return 1
	}
	inventory, err := BuildInventory(db)
	if err != nil {
		fmt.Fprintln(stderr, "inventory failed:", err)
		return 1
	}
	if len(inventory.UnknownTables) > 0 {
		fmt.Fprintln(stderr, "purge refused: unknown database tables require manual classification")
		printUnknownTables(stderr, inventory)
		return 1
	}
	if header.DatabaseFingerprint != inventory.DatabaseFingerprint {
		fmt.Fprintln(stderr, "archive database fingerprint does not match the current control plane")
		return 1
	}
	if !archiveCoversInventory(header, inventory) {
		fmt.Fprintln(stderr, "archive does not exactly cover the current legacy data inventory")
		return 1
	}
	printPurgePlan(stdout, inventory)
	if !*execute {
		fmt.Fprintln(stdout, "dry_run=true")
		fmt.Fprintf(stdout, "execute with --execute --confirm %s\n", inventory.DatabaseFingerprint)
		return 0
	}
	if *confirm != inventory.DatabaseFingerprint {
		fmt.Fprintln(stderr, "--confirm must exactly match the current database fingerprint")
		return 2
	}
	if db.Dialector.Name() != "sqlite" {
		fmt.Fprintln(stderr, "destructive purge is currently supported only for SQLite; use a DBA-reviewed migration for MySQL or PostgreSQL")
		return 1
	}
	if err := Purge(db, inventory); err != nil {
		fmt.Fprintln(stderr, "purge failed:", err)
		return 1
	}
	receipt := map[string]any{
		"version": archiveVersion, "purged_at": time.Now().UTC().Format(time.RFC3339),
		"database_fingerprint": inventory.DatabaseFingerprint, "archive": *archivePath,
		"legacy_tables": inventory.LegacyTables, "legacy_user_columns": inventory.LegacyUserColumns,
	}
	receiptPath := *archivePath + ".purge-receipt.json"
	encoded, _ := json.MarshalIndent(receipt, "", "  ")
	if err := os.WriteFile(receiptPath, append(encoded, '\n'), 0600); err != nil {
		fmt.Fprintln(stderr, "write purge receipt:", err)
		return 1
	}
	fmt.Fprintln(stdout, "purged=true")
	fmt.Fprintln(stdout, "receipt="+receiptPath)
	return 0
}

func BuildInventory(db *gorm.DB) (*Inventory, error) {
	if db == nil {
		return nil, errors.New("database is nil")
	}
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return nil, err
	}
	sort.Strings(tables)
	legacyTables := make([]InventoryTable, 0)
	unknownTables := make([]InventoryTable, 0)
	for _, table := range tables {
		if _, protected := protectedTables[table]; protected || strings.HasPrefix(table, "sqlite_") {
			continue
		}
		var count int64
		if err := db.Raw("SELECT COUNT(*) FROM " + quoteIdentifier(db, table)).Scan(&count).Error; err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		contentHash, err := tableContentHash(db, table, nil)
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", table, err)
		}
		columns, err := archiveColumns(db, table, nil)
		if err != nil {
			return nil, fmt.Errorf("inspect schema %s: %w", table, err)
		}
		entry := InventoryTable{Name: table, RowCount: count, ContentHash: contentHash, Columns: columns}
		if _, known := knownLegacyTables[table]; known {
			legacyTables = append(legacyTables, entry)
		} else {
			unknownTables = append(unknownTables, entry)
		}
	}
	presentColumns := make([]string, 0, len(legacyUserColumns))
	if db.Migrator().HasTable("users") {
		for _, column := range legacyUserColumns {
			if db.Migrator().HasColumn("users", column) {
				presentColumns = append(presentColumns, column)
			}
		}
	}
	legacyUserDataHash := ""
	var legacyUserRowCount int64
	if len(presentColumns) > 0 {
		if err := db.Table("users").Count(&legacyUserRowCount).Error; err != nil {
			return nil, fmt.Errorf("count legacy users rows: %w", err)
		}
		legacyUserDataHash, err = tableContentHash(db, "users", append([]string{"id"}, presentColumns...))
		if err != nil {
			return nil, fmt.Errorf("hash legacy users columns: %w", err)
		}
	}
	fingerprint, err := databaseFingerprint(db)
	if err != nil {
		return nil, err
	}
	return &Inventory{
		Version: archiveVersion, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		DatabaseDriver: db.Dialector.Name(), DatabaseFingerprint: fingerprint,
		LegacyTables: legacyTables, UnknownTables: unknownTables,
		LegacyUserColumns: presentColumns, LegacyUserRowCount: legacyUserRowCount,
		LegacyUserDataHash: legacyUserDataHash,
	}, nil
}

func WriteArchive(db *gorm.DB, inventory *Inventory, outputPath string) (string, error) {
	if inventory == nil || inventory.DatabaseFingerprint == "" {
		return "", errors.New("inventory is invalid")
	}
	if len(inventory.UnknownTables) > 0 {
		return "", errors.New("unknown database tables require manual classification before archive")
	}
	absolute, err := filepath.Abs(outputPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
		return "", err
	}
	temporary := absolute + ".partial"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	writer := bufio.NewWriter(io.MultiWriter(file, digest))
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(temporary)
	}
	if err := writeArchiveJSON(db, inventory, writer); err != nil {
		cleanup()
		return "", err
	}
	if err := writer.Flush(); err != nil {
		cleanup()
		return "", err
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	if err := os.Rename(temporary, absolute); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	checksum := hex.EncodeToString(digest.Sum(nil))
	checksumLine := checksum + "  " + filepath.Base(absolute) + "\n"
	if err := os.WriteFile(absolute+".sha256", []byte(checksumLine), 0600); err != nil {
		return "", err
	}
	return checksum, nil
}

func writeArchiveJSON(db *gorm.DB, inventory *Inventory, writer io.Writer) error {
	metadata := map[string]any{
		"version": inventory.Version, "created_at": inventory.CreatedAt,
		"database_driver": inventory.DatabaseDriver, "database_fingerprint": inventory.DatabaseFingerprint,
		"legacy_user_columns": inventory.LegacyUserColumns, "legacy_user_row_count": inventory.LegacyUserRowCount,
		"legacy_user_data_hash": inventory.LegacyUserDataHash,
	}
	encodedMetadata, _ := json.Marshal(metadata)
	if _, err := fmt.Fprintf(writer, "{\"metadata\":%s,\"legacy_tables\":[", encodedMetadata); err != nil {
		return err
	}
	for index, table := range inventory.LegacyTables {
		if index > 0 {
			_, _ = io.WriteString(writer, ",")
		}
		if err := writeArchiveTable(db, writer, table.Name, table.RowCount, table.ContentHash, table.Columns, nil); err != nil {
			return err
		}
	}
	_, _ = io.WriteString(writer, "],\"legacy_user_data\":")
	if len(inventory.LegacyUserColumns) == 0 {
		_, _ = io.WriteString(writer, "null")
	} else {
		selectedColumns := append([]string{"id"}, inventory.LegacyUserColumns...)
		columns, err := archiveColumns(db, "users", selectedColumns)
		if err != nil {
			return err
		}
		if err := writeArchiveTable(db, writer, "users", inventory.LegacyUserRowCount, inventory.LegacyUserDataHash, columns, selectedColumns); err != nil {
			return err
		}
	}
	_, err := io.WriteString(writer, "}\n")
	return err
}

func writeArchiveTable(db *gorm.DB, writer io.Writer, table string, rowCount int64, expectedHash string, schemaColumns []ArchiveColumn, selectedColumns []string) error {
	nameJSON, _ := json.Marshal(table)
	columnsJSON, _ := json.Marshal(schemaColumns)
	if _, err := fmt.Fprintf(writer, "{\"name\":%s,\"row_count\":%d,\"content_hash\":%q,\"columns\":%s,\"rows\":[", nameJSON, rowCount, expectedHash, columnsJSON); err != nil {
		return err
	}
	query := "SELECT * FROM " + quoteIdentifier(db, table)
	if len(selectedColumns) > 0 {
		quoted := make([]string, 0, len(selectedColumns))
		for _, column := range selectedColumns {
			quoted = append(quoted, quoteIdentifier(db, column))
		}
		query = "SELECT " + strings.Join(quoted, ",") + " FROM " + quoteIdentifier(db, table)
	}
	rows, err := db.Raw(query).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	first := true
	contentDigest := sha256.New()
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return err
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			row[column] = archiveValue(values[index])
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return err
		}
		_, _ = contentDigest.Write(encoded)
		_, _ = contentDigest.Write([]byte{'\n'})
		if !first {
			_, _ = io.WriteString(writer, ",")
		}
		first = false
		if _, err := writer.Write(encoded); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if actualHash := hex.EncodeToString(contentDigest.Sum(nil)); actualHash != expectedHash {
		return fmt.Errorf("%s changed while it was being archived", table)
	}
	_, err = io.WriteString(writer, "]}")
	return err
}

func tableContentHash(db *gorm.DB, table string, selectedColumns []string) (string, error) {
	query := "SELECT * FROM " + quoteIdentifier(db, table)
	if len(selectedColumns) > 0 {
		quoted := make([]string, 0, len(selectedColumns))
		for _, column := range selectedColumns {
			quoted = append(quoted, quoteIdentifier(db, column))
		}
		query = "SELECT " + strings.Join(quoted, ",") + " FROM " + quoteIdentifier(db, table)
	}
	rows, err := db.Raw(query).Rows()
	if err != nil {
		return "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return "", err
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			row[column] = archiveValue(values[index])
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			return "", err
		}
		_, _ = digest.Write(encoded)
		_, _ = digest.Write([]byte{'\n'})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func archiveColumns(db *gorm.DB, table string, selectedColumns []string) ([]ArchiveColumn, error) {
	types, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return nil, err
	}
	selected := make(map[string]struct{}, len(selectedColumns))
	for _, name := range selectedColumns {
		selected[name] = struct{}{}
	}
	columns := make([]ArchiveColumn, 0, len(types))
	for _, columnType := range types {
		if len(selected) > 0 {
			if _, ok := selected[columnType.Name()]; !ok {
				continue
			}
		}
		nullable, _ := columnType.Nullable()
		primaryKey, _ := columnType.PrimaryKey()
		columns = append(columns, ArchiveColumn{
			Name: columnType.Name(), DatabaseType: columnType.DatabaseTypeName(),
			Nullable: nullable, PrimaryKey: primaryKey,
		})
	}
	return columns, nil
}

func VerifyArchive(path string) (*archiveHeader, error) {
	checksumBytes, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return nil, err
	}
	parts := strings.Fields(string(checksumBytes))
	if len(parts) < 1 || len(parts[0]) != sha256.Size*2 {
		return nil, errors.New("archive checksum file is invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return nil, err
	}
	if !strings.EqualFold(parts[0], hex.EncodeToString(digest.Sum(nil))) {
		return nil, errors.New("archive checksum mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	header, legacyUser, err := verifyArchiveBody(json.NewDecoder(file))
	if err != nil {
		return nil, err
	}
	if header.Version != archiveVersion || header.DatabaseFingerprint == "" {
		return nil, errors.New("archive metadata is invalid")
	}
	for _, table := range header.LegacyTables {
		if table.Name == "" || len(table.ContentHash) != sha256.Size*2 {
			return nil, errors.New("archive table metadata is invalid")
		}
	}
	if len(header.LegacyUserColumns) > 0 && (legacyUser == nil || legacyUser.ContentHash != header.LegacyUserDataHash || legacyUser.RowCount != header.LegacyUserRowCount) {
		return nil, errors.New("archive legacy user rows do not match metadata")
	}
	if len(header.LegacyUserColumns) > 0 && len(header.LegacyUserDataHash) != sha256.Size*2 {
		return nil, errors.New("archive legacy user metadata is invalid")
	}
	return header, nil
}

func verifyArchiveBody(decoder *json.Decoder) (*archiveHeader, *archiveTableRef, error) {
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, nil, errors.New("archive body must be a JSON object")
	}
	header := &archiveHeader{}
	var legacyUser *archiveTableRef
	seenMetadata, seenTables, seenUser := false, false, false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, nil, errors.New("archive object key is invalid")
		}
		switch key {
		case "metadata":
			if seenMetadata || decoder.Decode(header) != nil {
				return nil, nil, errors.New("archive metadata is invalid")
			}
			seenMetadata = true
		case "legacy_tables":
			if seenTables {
				return nil, nil, errors.New("archive legacy_tables is duplicated")
			}
			tables, err := verifyArchiveTableArray(decoder)
			if err != nil {
				return nil, nil, err
			}
			header.LegacyTables = tables
			seenTables = true
		case "legacy_user_data":
			if seenUser {
				return nil, nil, errors.New("archive legacy_user_data is duplicated")
			}
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				return nil, nil, err
			}
			if string(raw) != "null" {
				entry, err := verifyArchiveTableRaw(raw)
				if err != nil {
					return nil, nil, err
				}
				legacyUser = &entry
			}
			seenUser = true
		default:
			return nil, nil, fmt.Errorf("unknown archive field %q", key)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, nil, err
	}
	if !seenMetadata || !seenTables || !seenUser {
		return nil, nil, errors.New("archive body is incomplete")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, nil, errors.New("archive contains trailing JSON data")
	}
	return header, legacyUser, nil
}

func verifyArchiveTableArray(decoder *json.Decoder) ([]archiveTableRef, error) {
	start, err := decoder.Token()
	if err != nil || start != json.Delim('[') {
		return nil, errors.New("legacy_tables must be an array")
	}
	tables := make([]archiveTableRef, 0)
	seenNames := make(map[string]struct{})
	for decoder.More() {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		entry, err := verifyArchiveTableRaw(raw)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seenNames[entry.Name]; duplicate {
			return nil, fmt.Errorf("archive table %s is duplicated", entry.Name)
		}
		seenNames[entry.Name] = struct{}{}
		tables = append(tables, entry)
	}
	_, err = decoder.Token()
	return tables, err
}

func verifyArchiveTableRaw(raw json.RawMessage) (archiveTableRef, error) {
	var table struct {
		Name        string            `json:"name"`
		RowCount    int64             `json:"row_count"`
		ContentHash string            `json:"content_hash"`
		Columns     []ArchiveColumn   `json:"columns"`
		Rows        []json.RawMessage `json:"rows"`
	}
	tableDecoder := json.NewDecoder(strings.NewReader(string(raw)))
	tableDecoder.DisallowUnknownFields()
	if err := tableDecoder.Decode(&table); err != nil {
		return archiveTableRef{}, err
	}
	if err := ensureJSONEOF(tableDecoder); err != nil {
		return archiveTableRef{}, err
	}
	digest := sha256.New()
	if table.Name == "" || len(table.Columns) == 0 {
		return archiveTableRef{}, errors.New("archive table schema is missing")
	}
	columnNames := make(map[string]struct{}, len(table.Columns))
	for _, column := range table.Columns {
		if column.Name == "" || column.DatabaseType == "" {
			return archiveTableRef{}, errors.New("archive table schema is invalid")
		}
		columnNames[column.Name] = struct{}{}
	}
	for _, row := range table.Rows {
		var value map[string]any
		rowDecoder := json.NewDecoder(strings.NewReader(string(row)))
		rowDecoder.UseNumber()
		if err := rowDecoder.Decode(&value); err != nil {
			return archiveTableRef{}, errors.New("archive row must be a JSON object")
		}
		if err := ensureJSONEOF(rowDecoder); err != nil {
			return archiveTableRef{}, err
		}
		if len(value) != len(columnNames) {
			return archiveTableRef{}, fmt.Errorf("archive row schema mismatch for %s", table.Name)
		}
		for name := range value {
			if _, ok := columnNames[name]; !ok {
				return archiveTableRef{}, fmt.Errorf("archive row contains unknown column %s.%s", table.Name, name)
			}
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return archiveTableRef{}, err
		}
		_, _ = digest.Write(canonical)
		_, _ = digest.Write([]byte{'\n'})
	}
	if table.RowCount >= 0 && int64(len(table.Rows)) != table.RowCount {
		return archiveTableRef{}, fmt.Errorf("archive row count mismatch for %s", table.Name)
	}
	actualHash := hex.EncodeToString(digest.Sum(nil))
	if table.ContentHash != actualHash {
		return archiveTableRef{}, fmt.Errorf("archive content hash mismatch for %s", table.Name)
	}
	return archiveTableRef{Name: table.Name, RowCount: table.RowCount, ContentHash: table.ContentHash, Columns: table.Columns}, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("archive JSON value contains trailing data")
	}
	return nil
}

func Purge(db *gorm.DB, inventory *Inventory) error {
	if db == nil || db.Dialector.Name() != "sqlite" {
		return errors.New("destructive purge is currently supported only for SQLite")
	}
	if inventory == nil || len(inventory.UnknownTables) > 0 {
		return errors.New("unknown database tables require manual classification before purge")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		current, err := BuildInventory(tx)
		if err != nil {
			return err
		}
		if !inventoriesEqual(inventory, current) {
			return errors.New("legacy data changed after purge validation")
		}
		for _, table := range inventory.LegacyTables {
			if _, known := knownLegacyTables[table.Name]; !known {
				return fmt.Errorf("refusing to purge unknown legacy table %s", table.Name)
			}
			if _, protected := protectedTables[table.Name]; protected {
				return fmt.Errorf("refusing to purge protected table %s", table.Name)
			}
			if err := tx.Migrator().DropTable(table.Name); err != nil {
				return err
			}
		}
		for _, column := range inventory.LegacyUserColumns {
			if !contains(legacyUserColumns, column) {
				return fmt.Errorf("refusing to purge unknown users column %s", column)
			}
			statement := "ALTER TABLE " + quoteIdentifier(tx, "users") + " DROP COLUMN " + quoteIdentifier(tx, column)
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func databaseFingerprint(db *gorm.DB) (string, error) {
	identity := struct {
		Driver string `json:"driver"`
		Setup  []struct {
			ID            uint   `json:"id"`
			Version       string `json:"version"`
			InitializedAt int64  `json:"initialized_at"`
		} `json:"setup"`
		RootIDs []int `json:"root_ids"`
	}{Driver: db.Dialector.Name()}
	if db.Migrator().HasTable("setups") {
		if err := db.Table("setups").Order("id").Find(&identity.Setup).Error; err != nil {
			return "", err
		}
	}
	if db.Migrator().HasTable("users") {
		if err := db.Table("users").Where("role = ?", common.RoleRootUser).Order("id").Pluck("id", &identity.RootIDs).Error; err != nil {
			return "", err
		}
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func quoteIdentifier(db *gorm.DB, value string) string {
	var builder strings.Builder
	db.Dialector.QuoteTo(&builder, value)
	return builder.String()
}

func archiveValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		if utf8.Valid(typed) {
			return string(typed)
		}
		return map[string]string{"base64": base64.StdEncoding.EncodeToString(typed)}
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	default:
		return typed
	}
}

func printPurgePlan(writer io.Writer, inventory *Inventory) {
	fmt.Fprintf(writer, "fingerprint=%s\n", inventory.DatabaseFingerprint)
	for _, table := range inventory.LegacyTables {
		fmt.Fprintf(writer, "drop_table=%s rows=%d\n", table.Name, table.RowCount)
	}
	for _, column := range inventory.LegacyUserColumns {
		fmt.Fprintf(writer, "drop_column=users.%s\n", column)
	}
}

func printUnknownTables(writer io.Writer, inventory *Inventory) {
	for _, table := range inventory.UnknownTables {
		fmt.Fprintf(writer, "unknown_table=%s rows=%d\n", table.Name, table.RowCount)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func archiveCoversInventory(header *archiveHeader, inventory *Inventory) bool {
	if header == nil || inventory == nil || len(header.LegacyTables) != len(inventory.LegacyTables) {
		return false
	}
	archivedTables := make(map[string]archiveTableRef, len(header.LegacyTables))
	for _, table := range header.LegacyTables {
		archivedTables[table.Name] = table
	}
	for _, table := range inventory.LegacyTables {
		archived, exists := archivedTables[table.Name]
		if !exists || archived.RowCount != table.RowCount || archived.ContentHash != table.ContentHash || !columnsEqual(archived.Columns, table.Columns) {
			return false
		}
	}
	if len(header.LegacyUserColumns) != len(inventory.LegacyUserColumns) {
		return false
	}
	for _, column := range inventory.LegacyUserColumns {
		if !contains(header.LegacyUserColumns, column) {
			return false
		}
	}
	return header.LegacyUserRowCount == inventory.LegacyUserRowCount && header.LegacyUserDataHash == inventory.LegacyUserDataHash
}

func inventoriesEqual(expected *Inventory, current *Inventory) bool {
	if expected == nil || current == nil || expected.DatabaseFingerprint != current.DatabaseFingerprint || len(current.UnknownTables) > 0 {
		return false
	}
	tables := make([]archiveTableRef, 0, len(expected.LegacyTables))
	for _, table := range expected.LegacyTables {
		tables = append(tables, archiveTableRef{Name: table.Name, RowCount: table.RowCount, ContentHash: table.ContentHash, Columns: table.Columns})
	}
	return archiveCoversInventory(&archiveHeader{
		LegacyTables: tables, LegacyUserColumns: expected.LegacyUserColumns,
		LegacyUserRowCount: expected.LegacyUserRowCount, LegacyUserDataHash: expected.LegacyUserDataHash,
	}, current)
}

func columnsEqual(left []ArchiveColumn, right []ArchiveColumn) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
