package controller

import (
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/01121531/subandnew-api/model"
	"github.com/01121531/subandnew-api/service/mailbox"
	"github.com/gin-gonic/gin"
)

func mailboxImportInput(c *gin.Context) (string, []byte, bool) {
	media, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if media == "multipart/form-data" {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12<<20)
		if err := c.Request.ParseMultipartForm(12 << 20); err != nil {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, false
		}
		defer c.Request.MultipartForm.RemoveAll()
		files := c.Request.MultipartForm.File["file"]
		if len(files) != 1 {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, false
		}
		file, err := files[0].Open()
		if err != nil {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, false
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
		if err != nil || len(data) > 10<<20 {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, false
		}
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(files[0].Filename)), ".")
		if format == "txt" {
			format = "text"
		}
		return format, data, true
	}
	var input struct {
		Format string `json:"format"`
		Text   string `json:"text"`
	}
	if !mailboxDecode(c, &input) {
		return "", nil, false
	}
	return input.Format, []byte(input.Text), true
}
func ImportMailboxAccounts(preview bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		format, data, ok := mailboxImportInput(c)
		if !ok {
			return
		}
		if preview {
			result, err := mailboxService().PreviewImport(c.Request.Context(), mailboxActor(c), format, data)
			if err != nil {
				mailboxFailure(c, err)
				return
			}
			mailboxSuccess(c, result)
			return
		}
		result, err := mailboxService().Import(c.Request.Context(), mailboxActor(c), format, data)
		if err != nil {
			mailboxFailure(c, err)
			return
		}
		mailboxSuccess(c, result)
	}
}
func ListMailboxAccounts(c *gin.Context) {
	q, ok := mailboxListQuery(c)
	if !ok {
		return
	}
	data, err := mailboxService().ListAccounts(c.Request.Context(), mailboxActor(c), q)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func GetMailboxAccount(c *gin.Context) {
	id := mailboxID(c)
	if id == 0 {
		return
	}
	c.Set("mailbox_account_id", id)
	data, err := mailboxService().GetAccount(c.Request.Context(), mailboxActor(c), id)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func GetMailboxCredentials(c *gin.Context) {
	id := mailboxID(c)
	if id == 0 {
		return
	}
	c.Set("mailbox_account_id", id)
	var input struct {
		Kind string `json:"kind"`
	}
	if !mailboxDecode(c, &input) {
		return
	}
	data, err := mailboxService().Credentials(c.Request.Context(), mailboxActor(c), id, input.Kind)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	if _, err := mailboxService().AccountForActor(mailboxActor(c), id, true); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func AssignMailboxAccounts(c *gin.Context) {
	var input mailbox.AssignInput
	if !mailboxDecode(c, &input) {
		return
	}
	if err := mailboxService().Assign(c.Request.Context(), mailboxActor(c), input); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, gin.H{})
}
func ListMailboxOperators(c *gin.Context) {
	q, ok := mailboxListQuery(c)
	if !ok {
		return
	}
	data, err := mailboxService().ListOperators(c.Request.Context(), mailboxActor(c), q)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func MailboxOperatorOptions(c *gin.Context) {
	mailboxOperatorChoices(c, true)
}
func MailboxAccountOperators(c *gin.Context) {
	mailboxOperatorChoices(c, false)
}
func mailboxOperatorChoices(c *gin.Context, enabledOnly bool) {
	type option struct {
		ID          int64  `json:"id"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
	}
	items := []option{}
	query := model.DB.Model(&model.MailboxOperator{}).Select("id, username, display_name")
	if enabledOnly {
		query = query.Where("enabled = ?", true)
	}
	if err := query.Order("id ASC").Find(&items).Error; err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, items)
}
func SaveMailboxOperator(c *gin.Context) {
	var id int64
	if c.Param("id") != "" {
		id = mailboxID(c)
		if id == 0 {
			return
		}
	}
	var input mailbox.OperatorInput
	if !mailboxDecode(c, &input) {
		return
	}
	data, err := mailboxService().SaveOperator(c.Request.Context(), mailboxActor(c), id, input)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func ResetMailboxOperatorPassword(c *gin.Context) {
	id := mailboxID(c)
	if id == 0 {
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if !mailboxDecode(c, &input) {
		return
	}
	password, err := mailboxService().ResetPassword(c.Request.Context(), mailboxActor(c), id, input.Password)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, gin.H{"password": password})
}
func RevokeMailboxOperatorSessions(c *gin.Context) {
	id := mailboxID(c)
	if id == 0 {
		return
	}
	if err := mailboxService().RevokeSessions(c.Request.Context(), mailboxActor(c), id); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, gin.H{})
}
func ListMailboxSubmissions(c *gin.Context) {
	q, ok := mailboxListQuery(c)
	if !ok {
		return
	}
	data, err := mailboxService().ListSubmissions(c.Request.Context(), mailboxActor(c), q)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func ReviewMailboxSubmission(c *gin.Context) {
	id := mailboxID(c)
	if id == 0 {
		return
	}
	var input mailbox.ReviewInput
	if !mailboxDecode(c, &input) {
		return
	}
	if err := mailboxService().Review(c.Request.Context(), mailboxActor(c), id, input); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, gin.H{})
}
func ListMailboxAudits(c *gin.Context) {
	q, ok := mailboxListQuery(c)
	if !ok {
		return
	}
	query := model.DB.Model(&model.MailboxAudit{})
	if q.OperatorID > 0 {
		query = query.Where("operator_id = ?", q.OperatorID)
	}
	if q.Search != "" {
		query = query.Where("action LIKE ?", "%"+q.Search+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		mailboxFailure(c, err)
		return
	}
	items := []model.MailboxAudit{}
	if err := query.Order("id DESC").Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&items).Error; err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, mailbox.Page[model.MailboxAudit]{Items: items, Total: total, Page: q.Page, PageSize: q.PageSize, HasMore: int64(q.Page*q.PageSize) < total})
}
