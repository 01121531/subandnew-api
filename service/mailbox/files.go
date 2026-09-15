package mailbox

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/authz"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const mailboxMaxImageBytes int64 = 10 * 1024 * 1024
const mailboxDraftTTL = 24 * time.Hour
const mailboxCleanupBatchSize = 100
const mailboxOrphanScanLimit = 1000
const mailboxOrphanCursorLimit = 8

type AttachmentView struct {
	ID          string `json:"id"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	CreatedAt   int64  `json:"created_at"`
	ExpiresAt   int64  `json:"expires_at"`
	DeletedAt   int64  `json:"deleted_at"`
}

func attachmentView(file model.MailboxAttachment) AttachmentView {
	return AttachmentView{ID: file.ID, ContentType: file.ContentType, Size: file.Size, Width: file.Width, Height: file.Height, CreatedAt: file.CreatedAt, ExpiresAt: file.ExpiresAt, DeletedAt: file.DeletedAt}
}

func validAttachmentID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validMailboxStorageKey(key string) bool {
	ext := filepath.Ext(key)
	return (ext == ".png" || ext == ".jpg" || ext == ".tmp") && validAttachmentID(strings.TrimSuffix(key, ext))
}

func attachmentStorageKey(file model.MailboxAttachment) (string, error) {
	ext := ".png"
	if file.ContentType == "image/jpeg" {
		ext = ".jpg"
	} else if file.ContentType != "image/png" {
		return "", fail(500, "mailbox_storage_unavailable")
	}
	if !validAttachmentID(file.ID) || file.StorageKey != file.ID+ext {
		return "", fail(500, "mailbox_storage_unavailable")
	}
	return file.StorageKey, nil
}

// Walk from an anchored filesystem/volume root. Every child is checked through
// its parent's handle, so replacing a writable ancestor cannot redirect chmod.
func (s *Service) attachmentRoot() (*os.Root, error) {
	dir := s.StorageDir
	if dir == "" {
		dir = os.Getenv("MAILBOX_ATTACHMENT_DIR")
	}
	if dir == "" {
		dir = filepath.Join("data", "mailbox-attachments")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	anchor := filepath.VolumeName(abs) + string(os.PathSeparator)
	if abs == anchor {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	root, err := os.OpenRoot(anchor)
	if err != nil {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	for _, component := range strings.Split(strings.TrimPrefix(abs, anchor), string(os.PathSeparator)) {
		child, err := openMailboxChildRoot(root, component)
		root.Close()
		if err != nil {
			return nil, err
		}
		root = child
	}
	if err := root.Chmod(".", 0700); err != nil {
		root.Close()
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	return root, nil
}

type mailboxRootParent interface {
	Lstat(string) (os.FileInfo, error)
	Mkdir(string, os.FileMode) error
	OpenRoot(string) (*os.Root, error)
}

func openMailboxChildRoot(parent mailboxRootParent, name string) (*os.Root, error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	before, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		if err := parent.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fail(500, "mailbox_storage_unavailable")
		}
		before, err = parent.Lstat(name)
	}
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	opened, err := child.Stat(".")
	after, afterErr := parent.Lstat(name)
	if err != nil || afterErr != nil || !after.IsDir() || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(after, opened) {
		child.Close()
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	return child, nil
}

type mailboxImageBuffer struct{ bytes.Buffer }

func (b *mailboxImageBuffer) Write(p []byte) (int, error) {
	if int64(b.Len())+int64(len(p)) > mailboxMaxImageBytes {
		return 0, fail(413, "mailbox_attachment_too_large")
	}
	return b.Buffer.Write(p)
}

func sanitizeMailboxImage(reader io.Reader) ([]byte, string, int, int, error) {
	if reader == nil {
		return nil, "", 0, 0, fail(400, "mailbox_invalid_image")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, mailboxMaxImageBytes+1))
	if err != nil {
		return nil, "", 0, 0, fail(400, "mailbox_invalid_image")
	}
	if int64(len(raw)) > mailboxMaxImageBytes {
		return nil, "", 0, 0, fail(413, "mailbox_attachment_too_large")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") || config.Width <= 0 || config.Height <= 0 || config.Width > 10000 || config.Height > 10000 || int64(config.Width)*int64(config.Height) > 25000000 {
		return nil, "", 0, 0, fail(400, "mailbox_invalid_image")
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(raw))
	if err != nil || decodedFormat != format || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return nil, "", 0, 0, fail(400, "mailbox_invalid_image")
	}
	var output mailboxImageBuffer
	contentType := "image/png"
	if format == "jpeg" {
		contentType = "image/jpeg"
		err = jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&output, decoded)
	}
	if err != nil {
		return nil, "", 0, 0, fail(413, "mailbox_attachment_too_large")
	}
	return output.Bytes(), contentType, config.Width, config.Height, nil
}

func (s *Service) draftAssignment(actor Actor, id int64) (*model.MailboxAssignment, error) {
	if err := s.CheckActor(actor, authz.MailboxView); err != nil {
		return nil, err
	}
	if actor.Admin != nil {
		return nil, fail(403, "mailbox_permission_denied")
	}
	var assignment model.MailboxAssignment
	err := s.DB.Table("mailbox_assignments").Joins("JOIN mailbox_accounts ON mailbox_accounts.active_assignment_id = mailbox_assignments.id AND mailbox_accounts.id = mailbox_assignments.account_id").Where("mailbox_accounts.account_type = ?", s.pool()).Where("mailbox_assignments.id = ? AND mailbox_assignments.operator_id = ? AND mailbox_assignments.revoked_at = 0", id, actor.OperatorID).Select("mailbox_assignments.*").Take(&assignment).Error
	if err != nil {
		return nil, workflowNotFound(err)
	}
	if assignment.Status != StatusPending && assignment.Status != StatusRejected {
		return nil, fail(409, "mailbox_invalid_state")
	}
	return &assignment, nil
}

func (s *Service) UploadAttachment(ctx context.Context, actor Actor, assignmentID int64, reader io.Reader, accountTypes ...string) (*AttachmentView, error) {
	var err error
	s, err = s.withAccountType("", accountTypes...)
	if err != nil {
		return nil, err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	if _, err := s.draftAssignment(actor, assignmentID); err != nil {
		return nil, err
	}
	data, contentType, width, height, err := sanitizeMailboxImage(reader)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := s.attachmentRoot()
	if err != nil {
		return nil, err
	}
	defer root.Close()
	random, err := token()
	if err != nil {
		return nil, err
	}
	id := digest(random)
	ext := ".png"
	if contentType == "image/jpeg" {
		ext = ".jpg"
	}
	key, staging := id+ext, id+".tmp"
	file, err := root.OpenFile(staging, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	defer root.Remove(staging)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	// Publish the fully written inode without ever replacing an existing key.
	if err := root.Link(staging, key); err != nil {
		return nil, fail(500, "mailbox_storage_unavailable")
	}
	committed := false
	defer func() {
		if !committed {
			_ = root.Remove(key)
		}
	}()
	var attachment model.MailboxAttachment
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		s := s.WithDB(tx)
		if err := s.workflowActor(actor, authz.MailboxView); err != nil {
			return err
		}
		assignment, account, err := s.workflowAssignment(actor, assignmentID)
		if err != nil {
			return err
		}
		if assignment.Status != StatusPending && assignment.Status != StatusRejected {
			return fail(409, "mailbox_invalid_state")
		}
		now := s.Now().Unix()
		var count int64
		if err := tx.Model(&model.MailboxAttachment{}).Where("assignment_id = ? AND submission_id = 0 AND issue_id = 0 AND deleted_at = 0 AND expires_at > ?", assignmentID, now).Count(&count).Error; err != nil {
			return err
		}
		if count >= 10 {
			return fail(409, "mailbox_attachment_limit")
		}
		attachment = model.MailboxAttachment{ID: id, AssignmentID: assignmentID, OperatorID: actor.OperatorID, StorageKey: key, ContentType: contentType, Size: int64(len(data)), Width: width, Height: height, CreatedAt: now, ExpiresAt: now + int64(mailboxDraftTTL/time.Second)}
		if err := tx.Create(&attachment).Error; err != nil {
			return err
		}
		return s.Audit(actor, "upload_attachment", account.ID, assignmentID, 200, "")
	})
	if err != nil {
		return nil, err
	}
	committed = true
	if _, err := s.draftAssignment(actor, assignmentID); err != nil {
		return nil, err
	}
	view := attachmentView(attachment)
	return &view, nil
}

func (s *Service) attachmentForActor(actor Actor, id string) (*model.MailboxAttachment, error) {
	permission := authz.MailboxView
	if actor.Admin != nil && actor.Admin.Can(authz.MailboxReview) {
		permission = authz.MailboxReview
	}
	if err := s.CheckActor(actor, permission); err != nil {
		return nil, err
	}
	if !validAttachmentID(id) {
		return nil, fail(404, "mailbox_not_found")
	}
	var attachment model.MailboxAttachment
	if err := s.DB.Where("id = ?", id).First(&attachment).Error; err != nil {
		return nil, workflowNotFound(err)
	}
	var poolCount int64
	if err := s.DB.Table("mailbox_assignments AS t").Joins("JOIN mailbox_accounts AS a ON a.id = t.account_id").Where("t.id = ? AND a.account_type = ?", attachment.AssignmentID, s.pool()).Count(&poolCount).Error; err != nil {
		return nil, err
	}
	if poolCount != 1 {
		return nil, fail(404, "mailbox_not_found")
	}
	if attachment.IssueID != 0 {
		if attachment.SubmissionID != 0 {
			return nil, fail(404, "mailbox_not_found")
		}
		if err := s.CheckActor(actor, authz.MailboxReview); err != nil {
			return nil, err
		}
	}
	if actor.Admin == nil {
		if attachment.OperatorID != actor.OperatorID {
			return nil, fail(404, "mailbox_not_found")
		}
		var count int64
		if attachment.IssueID != 0 {
			if err := s.DB.Model(&model.MailboxIssue{}).Where("id = ? AND assignment_id = ? AND operator_id = ?", attachment.IssueID, attachment.AssignmentID, actor.OperatorID).Count(&count).Error; err != nil {
				return nil, err
			}
		} else if attachment.SubmissionID != 0 {
			if err := s.DB.Model(&model.MailboxSubmission{}).Where("id = ? AND assignment_id = ? AND operator_id = ?", attachment.SubmissionID, attachment.AssignmentID, actor.OperatorID).Count(&count).Error; err != nil {
				return nil, err
			}
		} else {
			if err := s.DB.Table("mailbox_assignments AS t").Joins("JOIN mailbox_accounts AS a ON a.active_assignment_id = t.id AND a.id = t.account_id").Where("t.id = ? AND t.operator_id = ? AND t.revoked_at = 0 AND t.status IN ?", attachment.AssignmentID, actor.OperatorID, []string{StatusPending, StatusRejected}).Count(&count).Error; err != nil {
				return nil, err
			}
		}
		if count != 1 {
			return nil, fail(404, "mailbox_not_found")
		}
	}
	if attachment.DeletedAt != 0 || attachment.ExpiresAt <= s.Now().Unix() {
		return nil, fail(410, "mailbox_attachment_expired")
	}
	return &attachment, nil
}

func (s *Service) ReadAttachment(ctx context.Context, actor Actor, id string, accountTypes ...string) ([]byte, string, error) {
	var err error
	s, err = s.withAccountType("", accountTypes...)
	if err != nil {
		return nil, "", err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	attachment, err := s.attachmentForActor(actor, id)
	if err != nil {
		return nil, "", err
	}
	key, err := attachmentStorageKey(*attachment)
	if err != nil {
		return nil, "", err
	}
	root, err := s.attachmentRoot()
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	info, err := root.Lstat(key)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != attachment.Size || info.Size() > mailboxMaxImageBytes {
		return nil, "", fail(410, "mailbox_attachment_unavailable")
	}
	file, err := root.Open(key)
	if err != nil {
		return nil, "", fail(410, "mailbox_attachment_unavailable")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, "", fail(410, "mailbox_attachment_unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(file, mailboxMaxImageBytes+1))
	if err != nil || int64(len(data)) != attachment.Size || int64(len(data)) > mailboxMaxImageBytes {
		return nil, "", fail(410, "mailbox_attachment_unavailable")
	}
	// Recheck outside a long-running snapshot after disk I/O; never return bytes
	// whose owner, session, assignment, or expiry changed while they were read.
	latest, err := s.attachmentForActor(actor, id)
	if err != nil {
		return nil, "", err
	}
	if latest.StorageKey != attachment.StorageKey || latest.ContentType != attachment.ContentType || latest.Size != attachment.Size {
		return nil, "", fail(410, "mailbox_attachment_unavailable")
	}
	return data, attachment.ContentType, nil
}

func removeMailboxFile(root *os.Root, key string) error {
	if !validMailboxStorageKey(key) {
		return fail(500, "mailbox_storage_unavailable")
	}
	info, err := root.Lstat(key)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fail(500, "mailbox_storage_unavailable")
	}
	// Removing a leaf never follows it; Root also confines parent traversal.
	if err := root.Remove(key); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Cleanup is an internal scheduler entry point, not an actor-facing operation.
func (s *Service) Cleanup(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s = s.WithDB(s.DB.WithContext(ctx))
	root, err := s.attachmentRoot()
	if err != nil {
		return err
	}
	defer root.Close()
	now := s.Now().Unix()
	// Freeze the cutoff and upper cursor for this cycle, without holding a
	// database snapshot open during filesystem I/O. New uploads expire later.
	var upper model.MailboxAttachment
	err = s.DB.Select("id, expires_at").Where("deleted_at = 0 AND expires_at <= ?", now).Order("expires_at DESC, id DESC").Take(&upper).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var firstErr error
	var cursor model.MailboxAttachment
	hasCursor := false
	for err == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		q := s.DB.Select("id, expires_at").Where("deleted_at = 0 AND expires_at <= ?", now).
			Where("expires_at < ? OR (expires_at = ? AND id <= ?)", upper.ExpiresAt, upper.ExpiresAt, upper.ID)
		if hasCursor {
			q = q.Where("expires_at > ? OR (expires_at = ? AND id > ?)", cursor.ExpiresAt, cursor.ExpiresAt, cursor.ID)
		}
		var expired []model.MailboxAttachment
		if err := q.Order("expires_at, id").Limit(mailboxCleanupBatchSize).Find(&expired).Error; err != nil {
			return err
		}
		if len(expired) == 0 {
			break
		}
		for _, candidate := range expired {
			if err := ctx.Err(); err != nil {
				return err
			}
			// Advance even on failure so corrupt rows cannot starve later pages.
			// Failed rows remain unmarked and are retried next cycle.
			cursor, hasCursor = candidate, true
			if err := s.cleanupExpiredMailboxAttachment(root, candidate.ID, now); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.cleanupMailboxOrphans(ctx, root, now); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return firstErr
}

func (s *Service) cleanupExpiredMailboxAttachment(root *os.Root, id string, now int64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var current model.MailboxAttachment
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if current.DeletedAt != 0 || current.ExpiresAt > now {
			return nil
		}
		// Acquire a write lock on SQLite before touching disk, racing safely
		// with submission's extension of the draft expiry.
		if err := tx.Model(&model.MailboxAttachment{}).Where("id = ? AND deleted_at = 0 AND expires_at <= ?", current.ID, now).Update("expires_at", gorm.Expr("expires_at")).Error; err != nil {
			return err
		}
		key, err := attachmentStorageKey(current)
		if err != nil {
			return err
		}
		if err := removeMailboxFile(root, key); err != nil {
			return err
		}
		if err := workflowCAS(tx.Model(&model.MailboxAttachment{}).Where("id = ? AND deleted_at = 0 AND expires_at <= ?", current.ID, now).Update("deleted_at", now)); err != nil {
			return err
		}
		return s.WithDB(tx).Audit(Actor{}, "expire_attachment", 0, current.AssignmentID, 200, "")
	})
}

type mailboxOrphanCursor struct {
	dir      *os.File
	identity os.FileInfo
	lastUsed time.Time
}

// Keep directory enumeration positions across hourly service instances. The
// cancellable gate serializes scans; at most eight directory handles survive a
// cycle. Cursors expire after two hours idle and reset at EOF or root replacement.
var mailboxOrphanScans = struct {
	gate    chan struct{}
	cursors map[string]*mailboxOrphanCursor
}{gate: make(chan struct{}, 1), cursors: make(map[string]*mailboxOrphanCursor)}

func (s *Service) cleanupMailboxOrphans(ctx context.Context, root *os.Root, now int64) error {
	select {
	case mailboxOrphanScans.gate <- struct{}{}:
		defer func() { <-mailboxOrphanScans.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key := filepath.Clean(root.Name())
	identity, err := root.Stat(".")
	if err != nil {
		return err
	}
	wallNow := time.Now()
	for name, cursor := range mailboxOrphanScans.cursors {
		if wallNow.Sub(cursor.lastUsed) > 2*time.Hour || (name == key && !os.SameFile(cursor.identity, identity)) {
			cursor.dir.Close()
			delete(mailboxOrphanScans.cursors, name)
		}
	}
	cursor := mailboxOrphanScans.cursors[key]
	if cursor == nil {
		if len(mailboxOrphanScans.cursors) >= mailboxOrphanCursorLimit {
			oldest := ""
			for name, candidate := range mailboxOrphanScans.cursors {
				if oldest == "" || candidate.lastUsed.Before(mailboxOrphanScans.cursors[oldest].lastUsed) {
					oldest = name
				}
			}
			mailboxOrphanScans.cursors[oldest].dir.Close()
			delete(mailboxOrphanScans.cursors, oldest)
		}
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		cursor = &mailboxOrphanCursor{dir: dir, identity: identity}
		mailboxOrphanScans.cursors[key] = cursor
	}
	cursor.lastUsed = wallNow
	var firstErr error
	for scanned := 0; scanned < mailboxOrphanScanLimit; {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := cursor.dir.ReadDir(mailboxCleanupBatchSize)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			cursor.dir.Close()
			delete(mailboxOrphanScans.cursors, key)
			return readErr
		}
		scanned += len(entries)
		keys := make([]string, 0, len(entries))
		for _, entry := range entries {
			if validMailboxStorageKey(entry.Name()) && entry.Type()&os.ModeType == 0 {
				keys = append(keys, entry.Name())
			}
		}
		referenced := make(map[string]bool, len(keys))
		if len(keys) > 0 {
			var stored []string
			if err := s.DB.Model(&model.MailboxAttachment{}).Where("storage_key IN ?", keys).Distinct("storage_key").Pluck("storage_key", &stored).Error; err != nil {
				return err
			}
			for _, name := range stored {
				referenced[name] = true
			}
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !validMailboxStorageKey(entry.Name()) || entry.Type()&os.ModeType != 0 || referenced[entry.Name()] {
				continue
			}
			info, err := root.Lstat(entry.Name())
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if !info.Mode().IsRegular() || info.ModTime().Unix() > now-int64(mailboxDraftTTL/time.Second) {
				continue
			}
			if err := removeMailboxFile(root, entry.Name()); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := s.Audit(Actor{}, "remove_orphan_attachment", 0, 0, 200, ""); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if errors.Is(readErr, io.EOF) {
			cursor.dir.Close()
			delete(mailboxOrphanScans.cursors, key)
			break
		}
	}
	return firstErr
}
