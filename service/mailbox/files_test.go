package mailbox

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func mailboxTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.NRGBA{R: 200, G: 40, B: 90, A: 255})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func mailboxTestPNGHeader(width, height uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	header := make([]byte, 17)
	copy(header, "IHDR")
	binary.BigEndian.PutUint32(header[4:8], width)
	binary.BigEndian.PutUint32(header[8:12], height)
	header[12], header[13] = 8, 6
	b.Write(header)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(header))
	return b.Bytes()
}

func mailboxTestWebP() []byte {
	// A synthetic opaque black 1x1 VP8L pixel: no transforms/cache/meta,
	// and five single-symbol Huffman trees. No external image fixture.
	payload := []byte{0x2f, 0, 0, 0, 0, 0, 0, 0, 0}
	bits := uint32(1<<3 | 1<<7 | 1<<11 | 1<<15 | 1<<17 | 255<<18 | 1<<26)
	binary.LittleEndian.PutUint32(payload[5:], bits)
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(4+8+len(payload)+1))
	b.WriteString("WEBPVP8L")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(payload)))
	b.Write(payload)
	b.WriteByte(0)
	return b.Bytes()
}

func TestMailboxFilesSanitizeAndBounds(t *testing.T) {
	data, contentType, width, height, err := sanitizeMailboxImage(bytes.NewReader(append(mailboxTestPNG(t), []byte("synthetic-private-trailing-metadata")...)))
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, 3, width)
	require.Equal(t, 2, height)
	require.NotContains(t, string(data), "synthetic-private-trailing-metadata")
	data, contentType, width, height, err = sanitizeMailboxImage(bytes.NewReader(mailboxTestWebP()))
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.Equal(t, 1, width)
	require.Equal(t, 1, height)
	_, format, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, "png", format)
	var jpg bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	comment := []byte("synthetic-private-jpeg-comment")
	withComment := append([]byte{0xff, 0xd8, 0xff, 0xfe, 0, byte(len(comment) + 2)}, comment...)
	withComment = append(withComment, jpg.Bytes()[2:]...)
	data, contentType, _, _, err = sanitizeMailboxImage(bytes.NewReader(withComment))
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", contentType)
	require.NotContains(t, string(data), string(comment))
	for name, input := range map[string][]byte{"text": []byte("not an image"), "svg": []byte("<svg/>"), "gif": []byte("GIF89a"), "truncated": mailboxTestPNG(t)[:35], "dimension": mailboxTestPNGHeader(10001, 1), "pixels": mailboxTestPNGHeader(6000, 5000), "zero": mailboxTestPNGHeader(0, 1)} {
		t.Run(name, func(t *testing.T) {
			_, _, _, _, err := sanitizeMailboxImage(bytes.NewReader(input))
			workflowTestStatus(t, err, 400)
		})
	}
	_, _, _, _, err = sanitizeMailboxImage(nil)
	workflowTestStatus(t, err, 400)
	_, _, _, _, err = sanitizeMailboxImage(io.LimitReader(mailboxZeroReader{}, mailboxMaxImageBytes+1))
	workflowTestStatus(t, err, 413)
	var bounded mailboxImageBuffer
	_, err = bounded.Write(make([]byte, mailboxMaxImageBytes))
	require.NoError(t, err)
	_, err = bounded.Write([]byte{1})
	workflowTestStatus(t, err, 413)
	require.EqualValues(t, mailboxMaxImageBytes, bounded.Len())
}

type mailboxZeroReader struct{}

func (mailboxZeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestMailboxFilesPrivateReadAndExpiry(t *testing.T) {
	s, admin, owner, other := workflowTestService(t)
	ctx := context.Background()
	a := workflowTestAccount(t, s, "files@example.test")
	view := workflowTestAssign(t, s, admin, owner, a.ID)
	file := workflowTestUpload(t, s, owner, view.AssignmentID)
	require.Len(t, file.ID, 64)
	require.Equal(t, s.Now().Unix()+86400, file.ExpiresAt)
	data, contentType, err := s.ReadAttachment(ctx, owner, file.ID)
	require.NoError(t, err)
	require.Equal(t, "image/png", contentType)
	require.EqualValues(t, len(data), file.Size)
	_, _, err = image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	_, _, err = s.ReadAttachment(ctx, admin, file.ID)
	require.NoError(t, err)
	_, _, err = s.ReadAttachment(ctx, other, file.ID)
	workflowTestStatus(t, err, 404)
	_, _, err = s.ReadAttachment(ctx, owner, "../outside.png")
	workflowTestStatus(t, err, 404)
	encoded, err := json.Marshal(file)
	require.NoError(t, err)
	for _, hidden := range []string{"storage_key", "operator_id", "assignment_id", "submission_id", "filename", s.StorageDir} {
		require.NotContains(t, string(encoded), hidden)
	}
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("id = ?", file.ID).Update("expires_at", s.Now().Unix()).Error)
	_, _, err = s.ReadAttachment(ctx, owner, file.ID)
	workflowTestStatus(t, err, 410)
	_, err = os.Stat(filepath.Join(s.StorageDir, file.ID+".png"))
	require.NoError(t, err)
	_, err = s.Submit(ctx, owner, view.AssignmentID, 1, []string{file.ID})
	workflowTestStatus(t, err, 400)
	require.NoError(t, s.Cleanup(ctx))
	_, err = os.Stat(filepath.Join(s.StorageDir, file.ID+".png"))
	require.True(t, errors.Is(err, os.ErrNotExist))
	var stored model.MailboxAttachment
	require.NoError(t, s.DB.Where("id = ?", file.ID).First(&stored).Error)
	require.Equal(t, s.Now().Unix(), stored.DeletedAt)
	require.NoError(t, s.Cleanup(ctx))
	_, _, err = s.ReadAttachment(ctx, owner, file.ID)
	workflowTestStatus(t, err, 410)
}

type mailboxHookReader struct {
	reader io.Reader
	hook   func()
	once   sync.Once
}

func (r *mailboxHookReader) Read(p []byte) (int, error) { r.once.Do(r.hook); return r.reader.Read(p) }

func TestMailboxFilesRecheckActorAndDBFailureCleanup(t *testing.T) {
	t.Run("revoked-during-decode", func(t *testing.T) {
		s, admin, owner, _ := workflowTestService(t)
		a := workflowTestAccount(t, s, "revoke-upload@example.test")
		view := workflowTestAssign(t, s, admin, owner, a.ID)
		reader := &mailboxHookReader{reader: bytes.NewReader(mailboxTestPNG(t)), hook: func() {
			require.NoError(t, s.DB.Where("token_hash = ?", owner.SessionHash).Delete(&model.MailboxSession{}).Error)
		}}
		_, err := s.UploadAttachment(context.Background(), owner, view.AssignmentID, reader)
		workflowTestStatus(t, err, 401)
		entries, err := os.ReadDir(s.StorageDir)
		require.NoError(t, err)
		require.Empty(t, entries)
		var count int64
		require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("revoked-after-file-read", func(t *testing.T) {
		s, admin, owner, _ := workflowTestService(t)
		a := workflowTestAccount(t, s, "revoke-read@example.test")
		view := workflowTestAssign(t, s, admin, owner, a.ID)
		file := workflowTestUpload(t, s, owner, view.AssignmentID)
		var calls atomic.Int32
		now := s.Now()
		s.Now = func() time.Time {
			if calls.Add(1) == 3 {
				require.NoError(t, s.DB.Where("token_hash = ?", owner.SessionHash).Delete(&model.MailboxSession{}).Error)
			}
			return now
		}
		data, _, err := s.ReadAttachment(context.Background(), owner, file.ID)
		workflowTestStatus(t, err, 401)
		require.Nil(t, data)
	})
	t.Run("audit-failure-rolls-back-file-and-row", func(t *testing.T) {
		s, admin, owner, _ := workflowTestService(t)
		a := workflowTestAccount(t, s, "failed-write@example.test")
		view := workflowTestAssign(t, s, admin, owner, a.ID)
		callback := "mailbox_test_reject_audit"
		require.NoError(t, s.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Table == "mailbox_audits" {
				tx.AddError(errors.New("synthetic audit failure"))
			}
		}))
		t.Cleanup(func() { _ = s.DB.Callback().Create().Remove(callback) })
		_, err := s.UploadAttachment(context.Background(), owner, view.AssignmentID, bytes.NewReader(mailboxTestPNG(t)))
		require.Error(t, err)
		entries, err := os.ReadDir(s.StorageDir)
		require.NoError(t, err)
		require.Empty(t, entries)
		var count int64
		require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Count(&count).Error)
		require.Zero(t, count)
	})
}

func TestMailboxFilesConcurrentDraftQuota(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	a := workflowTestAccount(t, s, "quota@example.test")
	view := workflowTestAssign(t, s, admin, owner, a.ID)
	for i := 0; i < 8; i++ {
		workflowTestUpload(t, s, owner, view.AssignmentID)
	}
	data := mailboxTestPNG(t)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = s.UploadAttachment(context.Background(), owner, view.AssignmentID, bytes.NewReader(data))
		}()
	}
	close(start)
	wg.Wait()
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Count(&count).Error)
	require.LessOrEqual(t, count, int64(10))
	for i := count; i < 10; i++ {
		workflowTestUpload(t, s, owner, view.AssignmentID)
	}
	_, err := s.UploadAttachment(context.Background(), owner, view.AssignmentID, bytes.NewReader(data))
	workflowTestStatus(t, err, 409)
	entries, err := os.ReadDir(s.StorageDir)
	require.NoError(t, err)
	require.Len(t, entries, 10)
}

func TestMailboxFilesCleanupOrphansAndPathConfinement(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	a := workflowTestAccount(t, s, "cleanup@example.test")
	view := workflowTestAssign(t, s, admin, owner, a.ID)
	file := workflowTestUpload(t, s, owner, view.AssignmentID)
	submission, err := s.Submit(context.Background(), owner, view.AssignmentID, 1, []string{file.ID})
	require.NoError(t, err)
	old := s.Now().Add(-25 * time.Hour)
	fresh := s.Now()
	orphan := filepath.Join(s.StorageDir, strings.Repeat("a", 64)+".tmp")
	unknown := filepath.Join(s.StorageDir, "uncontrolled.txt")
	recent := filepath.Join(s.StorageDir, strings.Repeat("b", 64)+".png")
	outside := filepath.Join(t.TempDir(), "outside.png")
	for _, path := range []string{orphan, unknown, recent, outside} {
		require.NoError(t, os.WriteFile(path, []byte("synthetic-test-content"), 0600))
		require.NoError(t, os.Chtimes(path, old, old))
	}
	require.NoError(t, os.Chtimes(recent, fresh, fresh))
	require.NoError(t, s.Cleanup(context.Background()))
	_, err = os.Stat(orphan)
	require.True(t, errors.Is(err, os.ErrNotExist))
	for _, path := range []string{unknown, recent, outside, filepath.Join(s.StorageDir, file.ID+".png")} {
		_, err := os.Stat(path)
		require.NoError(t, err)
	}
	_, _, err = s.ReadAttachment(context.Background(), owner, submission.Attachments[0].ID)
	require.NoError(t, err)
	bad := strings.Repeat("c", 64)
	require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: bad, AssignmentID: view.AssignmentID, OperatorID: owner.OperatorID, StorageKey: outside, ContentType: "image/png", ExpiresAt: s.Now().Unix() - 1}).Error)
	require.Error(t, s.Cleanup(context.Background()))
	content, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "synthetic-test-content", string(content))
	var corrupt model.MailboxAttachment
	require.NoError(t, s.DB.Where("id = ?", bad).First(&corrupt).Error)
	require.Zero(t, corrupt.DeletedAt)
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("id = ?", bad).Update("expires_at", s.Now().Unix()+1000).Error)
	_, _, err = s.ReadAttachment(context.Background(), admin, bad)
	workflowTestStatus(t, err, 500)
}

func TestMailboxFilesRejectSymlinks(t *testing.T) {
	s, admin, owner, _ := workflowTestService(t)
	a := workflowTestAccount(t, s, "symlink@example.test")
	view := workflowTestAssign(t, s, admin, owner, a.ID)
	file := workflowTestUpload(t, s, owner, view.AssignmentID)
	outside := filepath.Join(t.TempDir(), "outside.png")
	require.NoError(t, os.WriteFile(outside, mailboxTestPNG(t), 0600))
	link := filepath.Join(s.StorageDir, strings.Repeat("d", 64)+".png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("id = ?", file.ID).Updates(map[string]any{"id": strings.Repeat("d", 64), "storage_key": filepath.Base(link)}).Error)
	_, _, err := s.ReadAttachment(context.Background(), owner, strings.Repeat("d", 64))
	workflowTestStatus(t, err, 410)
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("id = ?", strings.Repeat("d", 64)).Update("expires_at", s.Now().Unix()-1).Error)
	require.Error(t, s.Cleanup(context.Background()))
	content, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, mailboxTestPNG(t), content)
	rootLink := filepath.Join(t.TempDir(), "root-link")
	require.NoError(t, os.Symlink(s.StorageDir, rootLink))
	s.StorageDir = rootLink
	_, err = s.attachmentRoot()
	workflowTestStatus(t, err, 500)
}

func TestMailboxFilesCleanupBoundedBatch(t *testing.T) {
	s, _, _, _ := workflowTestService(t)
	for i := 0; i < 305; i++ {
		id := digest(fmt.Sprintf("synthetic-expired-%d", i))
		require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: id, StorageKey: id + ".png", ContentType: "image/png", ExpiresAt: s.Now().Unix() - 1 - int64(i%3)}).Error)
	}
	var batchSizes []int
	callback := "mailbox_test_cleanup_batches"
	require.NoError(t, s.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if batch, ok := tx.Statement.Dest.(*[]model.MailboxAttachment); ok && len(*batch) > 0 {
			batchSizes = append(batchSizes, len(*batch))
		}
	}))
	t.Cleanup(func() { _ = s.DB.Callback().Query().Remove(callback) })
	require.NoError(t, s.Cleanup(context.Background()))
	var count int64
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("deleted_at > 0").Count(&count).Error)
	require.EqualValues(t, 305, count)
	require.Equal(t, []int{100, 100, 100, 5}, batchSizes)
	require.NoError(t, s.Cleanup(context.Background()))
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("deleted_at > 0").Count(&count).Error)
	require.EqualValues(t, 305, count)
	var audits int64
	require.NoError(t, s.DB.Model(&model.MailboxAudit{}).Where("action = ?", "expire_attachment").Count(&audits).Error)
	require.EqualValues(t, 305, audits)
}

func TestMailboxFilesCleanupPoisonRowsDoNotStarveLaterPages(t *testing.T) {
	s, _, _, _ := workflowTestService(t)
	outside := filepath.Join(t.TempDir(), "outside.png")
	require.NoError(t, os.WriteFile(outside, []byte("synthetic-outside-file"), 0600))
	for i := 0; i < 345; i++ {
		id := digest(fmt.Sprintf("synthetic-poison-backlog-%d", i))
		key, expires := id+".png", s.Now().Unix()-1
		if i < 110 {
			key, expires = outside, expires-1
		}
		require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: id, StorageKey: key, ContentType: "image/png", ExpiresAt: expires}).Error)
	}
	for i := 0; i < 2; i++ {
		workflowTestStatus(t, s.Cleanup(context.Background()), 500)
		var deleted, remaining int64
		require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("deleted_at > 0").Count(&deleted).Error)
		require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("deleted_at = 0").Count(&remaining).Error)
		require.EqualValues(t, 235, deleted)
		require.EqualValues(t, 110, remaining)
	}
	content, err := os.ReadFile(outside)
	require.NoError(t, err)
	require.Equal(t, "synthetic-outside-file", string(content))
}

func TestMailboxFilesCleanupCancellationBetweenBatches(t *testing.T) {
	s, _, _, _ := workflowTestService(t)
	for i := 0; i < 205; i++ {
		id := digest(fmt.Sprintf("synthetic-cancel-backlog-%d", i))
		require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: id, StorageKey: id + ".png", ContentType: "image/png", ExpiresAt: s.Now().Unix() - 1}).Error)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var batches int
	callback := "mailbox_test_cleanup_cancel"
	require.NoError(t, s.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if batch, ok := tx.Statement.Dest.(*[]model.MailboxAttachment); ok && len(*batch) > 0 {
			batches++
			if batches == 2 {
				cancel()
			}
		}
	}))
	t.Cleanup(func() { _ = s.DB.Callback().Query().Remove(callback) })
	require.ErrorIs(t, s.Cleanup(ctx), context.Canceled)
	var deleted int64
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("deleted_at > 0").Count(&deleted).Error)
	require.EqualValues(t, 100, deleted)
	require.NoError(t, s.Cleanup(context.Background()))
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("deleted_at > 0").Count(&deleted).Error)
	require.EqualValues(t, 205, deleted)
}

func TestMailboxFilesCleanupFreezesCutoffAndRechecksExpiry(t *testing.T) {
	s, _, _, _ := workflowTestService(t)
	cutoff := s.Now().Unix()
	for i := 0; i < 101; i++ {
		id := digest(fmt.Sprintf("synthetic-cutoff-%d", i))
		require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: id, StorageKey: id + ".png", ContentType: "image/png", ExpiresAt: cutoff - 1}).Error)
	}
	future := digest("synthetic-future-expiry")
	require.NoError(t, s.DB.Create(&model.MailboxAttachment{ID: future, StorageKey: future + ".png", ContentType: "image/png", ExpiresAt: cutoff + 1}).Error)
	callback := "mailbox_test_cleanup_extend_expiry"
	var extended string
	require.NoError(t, s.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if batch, ok := tx.Statement.Dest.(*[]model.MailboxAttachment); ok && len(*batch) > 0 && extended == "" {
			extended = (*batch)[0].ID
			require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("id = ?", extended).Update("expires_at", cutoff+86400).Error)
			require.NoError(t, os.WriteFile(filepath.Join(s.StorageDir, extended+".png"), []byte("synthetic-extended-file"), 0600))
			s.Now = func() time.Time { return time.Unix(cutoff+3600, 0) }
		}
	}))
	t.Cleanup(func() { _ = s.DB.Callback().Query().Remove(callback) })
	require.NoError(t, s.Cleanup(context.Background()))
	require.NotEmpty(t, extended)
	var deleted int64
	require.NoError(t, s.DB.Model(&model.MailboxAttachment{}).Where("deleted_at > 0").Count(&deleted).Error)
	require.EqualValues(t, 100, deleted)
	for _, id := range []string{extended, future} {
		var attachment model.MailboxAttachment
		require.NoError(t, s.DB.Where("id = ?", id).First(&attachment).Error)
		require.Zero(t, attachment.DeletedAt)
	}
	content, err := os.ReadFile(filepath.Join(s.StorageDir, extended+".png"))
	require.NoError(t, err)
	require.Equal(t, "synthetic-extended-file", string(content))
}

func TestMailboxFilesOrphanScanBoundedResumableAndBatched(t *testing.T) {
	s, _, _, _ := workflowTestService(t)
	root, err := s.attachmentRoot()
	require.NoError(t, err)
	root.Close()
	key := filepath.Clean(root.Name())
	t.Cleanup(func() {
		mailboxOrphanScans.gate <- struct{}{}
		defer func() { <-mailboxOrphanScans.gate }()
		if cursor := mailboxOrphanScans.cursors[key]; cursor != nil {
			cursor.dir.Close()
			delete(mailboxOrphanScans.cursors, key)
		}
	})
	old := s.Now().Add(-25 * time.Hour)
	var rows []model.MailboxAttachment
	for i := 0; i < mailboxOrphanScanLimit+105; i++ {
		id := digest(fmt.Sprintf("synthetic-retained-%d", i))
		path := filepath.Join(s.StorageDir, id+".png")
		require.NoError(t, os.WriteFile(path, []byte("synthetic-retained-file"), 0600))
		require.NoError(t, os.Chtimes(path, old, old))
		rows = append(rows, model.MailboxAttachment{ID: id, StorageKey: id + ".png", ContentType: "image/png", ExpiresAt: s.Now().Unix() + 86400})
	}
	require.NoError(t, s.DB.CreateInBatches(&rows, 100).Error)
	queries := 0
	seen := map[string]bool{}
	callback := "mailbox_test_orphan_lookup_batches"
	require.NoError(t, s.DB.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]string); ok && tx.Statement.Table == "mailbox_attachments" {
			queries++
			require.LessOrEqual(t, len(tx.Statement.Vars), mailboxCleanupBatchSize)
			for _, value := range tx.Statement.Vars {
				if name, ok := value.(string); ok {
					seen[name] = true
				}
			}
		}
	}))
	t.Cleanup(func() { _ = s.DB.Callback().Query().Remove(callback) })
	require.NoError(t, s.Cleanup(context.Background()))
	require.Equal(t, mailboxOrphanScanLimit/mailboxCleanupBatchSize, queries)
	require.Len(t, seen, mailboxOrphanScanLimit)
	var orphan string
	for _, row := range rows {
		if !seen[row.StorageKey] {
			orphan = row.ID
			break
		}
	}
	require.NotEmpty(t, orphan)
	require.NoError(t, s.DB.Where("id = ?", orphan).Delete(&model.MailboxAttachment{}).Error)
	queries = 0
	// A fresh Service must resume the same directory handle, not restart at the
	// first thousand retained files.
	next := New(s.DB)
	next.StorageDir, next.Now = s.StorageDir, s.Now
	require.NoError(t, next.Cleanup(context.Background()))
	require.Equal(t, 2, queries)
	require.Len(t, seen, len(rows))
	_, err = os.Stat(filepath.Join(s.StorageDir, orphan+".png"))
	require.ErrorIs(t, err, os.ErrNotExist)
	entries, err := os.ReadDir(s.StorageDir)
	require.NoError(t, err)
	require.Len(t, entries, len(rows)-1)
}

func TestMailboxFilesOrphanGateIsContextCancellable(t *testing.T) {
	s, _, _, _ := workflowTestService(t)
	root, err := s.attachmentRoot()
	require.NoError(t, err)
	defer root.Close()
	mailboxOrphanScans.gate <- struct{}{}
	defer func() { <-mailboxOrphanScans.gate }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, s.cleanupMailboxOrphans(ctx, root, s.Now().Unix()), context.Canceled)
}

type mailboxTestSwappingRoot struct {
	*os.Root
	beforeOpen func()
}

func (r mailboxTestSwappingRoot) OpenRoot(name string) (*os.Root, error) {
	r.beforeOpen()
	return r.Root.OpenRoot(name)
}

func TestMailboxFilesRootRejectsDirectorySwapBetweenCheckAndOpen(t *testing.T) {
	parent, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	defer parent.Close()
	require.NoError(t, parent.Mkdir("private", 0700))
	require.NoError(t, parent.Mkdir("unrelated", 0755))
	original, err := parent.Stat("unrelated")
	require.NoError(t, err)
	racing := mailboxTestSwappingRoot{Root: parent, beforeOpen: func() {
		require.NoError(t, parent.Rename("private", "original"))
		require.NoError(t, parent.Rename("unrelated", "private"))
	}}
	opened, err := openMailboxChildRoot(racing, "private")
	workflowTestStatus(t, err, 500)
	require.Nil(t, opened)
	after, err := parent.Stat("private")
	require.NoError(t, err)
	require.True(t, os.SameFile(original, after))
	require.Equal(t, original.Mode(), after.Mode())
}

func TestMailboxFilesRootCreatesAnchoredChildrenAndRejectsInvalidAncestors(t *testing.T) {
	s, _, _, _ := workflowTestService(t)
	s.StorageDir = filepath.Join(s.StorageDir, "nested", "private")
	root, err := s.attachmentRoot()
	require.NoError(t, err)
	info, err := root.Stat(".")
	require.NoError(t, err)
	root.Close()
	fromPath, err := os.Stat(s.StorageDir)
	require.NoError(t, err)
	require.True(t, os.SameFile(info, fromPath))
	fileParent := filepath.Join(t.TempDir(), "file-not-directory")
	require.NoError(t, os.WriteFile(fileParent, []byte("synthetic-file-ancestor"), 0600))
	s.StorageDir = filepath.Join(fileParent, "private")
	_, err = s.attachmentRoot()
	workflowTestStatus(t, err, 500)
	content, err := os.ReadFile(fileParent)
	require.NoError(t, err)
	require.Equal(t, "synthetic-file-ancestor", string(content))
	s.StorageDir = filepath.VolumeName(fileParent) + string(os.PathSeparator)
	_, err = s.attachmentRoot()
	workflowTestStatus(t, err, 500)
}
