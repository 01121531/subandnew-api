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

func mailboxImportInput(c *gin.Context) (string, []byte, string, bool, bool) {
	media, _, _ := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if media == "multipart/form-data" {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 12<<20)
		if err := c.Request.ParseMultipartForm(12 << 20); err != nil {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, "", false, false
		}
		defer c.Request.MultipartForm.RemoveAll()
		for key, values := range c.Request.MultipartForm.Value {
			if (key != "account_type" && key != "format" && key != "ignore_extra_fields") || len(values) != 1 {
				mailboxBadRequest(c, "mailbox_invalid_request")
				return "", nil, "", false, false
			}
		}
		ignoreValue := c.Request.FormValue("ignore_extra_fields")
		if ignoreValue != "" && ignoreValue != "true" && ignoreValue != "false" {
			mailboxBadRequest(c, "mailbox_invalid_request")
			return "", nil, "", false, false
		}
		accountType, ok := mailboxAccountType(c, c.Request.FormValue("account_type"))
		if !ok {
			return "", nil, "", false, false
		}
		files := c.Request.MultipartForm.File["file"]
		if len(files) != 1 || len(c.Request.MultipartForm.File) != 1 {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, "", false, false
		}
		file, err := files[0].Open()
		if err != nil {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, "", false, false
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
		if err != nil || len(data) > 10<<20 {
			mailboxBadRequest(c, "mailbox_invalid_file")
			return "", nil, "", false, false
		}
		format := strings.TrimPrefix(strings.ToLower(filepath.Ext(files[0].Filename)), ".")
		if format == "txt" {
			format = "text"
		}
		return format, data, accountType, ignoreValue == "true", true
	}
	var input struct {
		Format            string `json:"format"`
		Text              string `json:"text"`
		AccountType       string `json:"account_type"`
		IgnoreExtraFields bool   `json:"ignore_extra_fields"`
	}
	if !mailboxDecode(c, &input) {
		return "", nil, "", false, false
	}
	accountType, ok := mailboxAccountType(c, input.AccountType)
	return input.Format, []byte(input.Text), accountType, input.IgnoreExtraFields, ok
}
func ImportMailboxAccounts(preview bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		format, data, accountType, ignoreExtra, ok := mailboxImportInput(c)
		if !ok {
			return
		}
		service := mailboxService().WithImportExtraFields(ignoreExtra)
		if preview {
			result, err := service.PreviewImport(c.Request.Context(), mailboxActor(c), format, data, accountType)
			if err != nil {
				mailboxFailure(c, err)
				return
			}
			mailboxSuccess(c, result)
			return
		}
		result, err := service.Import(c.Request.Context(), mailboxActor(c), format, data, accountType)
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

func ArchiveMailboxAccounts(restore bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input mailbox.ArchiveInput
		if !mailboxDecode(c, &input) {
			return
		}
		accountType, ok := mailboxAccountType(c, input.AccountType)
		if !ok {
			return
		}
		input.AccountType = accountType
		if err := mailboxService().ArchiveAccounts(c.Request.Context(), mailboxActor(c), input, restore); err != nil {
			mailboxFailure(c, err)
			return
		}
		mailboxSuccess(c, gin.H{"updated": len(input.Items)})
	}
}
func GetMailboxAccount(c *gin.Context) {
	accountType, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return
	}
	id := mailboxID(c)
	if id == 0 {
		return
	}
	c.Set("mailbox_account_id", id)
	data, err := mailboxService().GetAccount(c.Request.Context(), mailboxActor(c), id, accountType)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}
func GetMailboxCredentials(c *gin.Context) {
	accountType, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return
	}
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
	data, err := mailboxService().Credentials(c.Request.Context(), mailboxActor(c), id, input.Kind, accountType)
	if err != nil {
		mailboxFailure(c, err)
		return
	}
	if _, err := mailboxService().AccountForActor(mailboxActor(c), id, true, accountType); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, data)
}

func ProvideMailboxTemporaryCVV(c *gin.Context) {
	accountType, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return
	}
	id := mailboxID(c)
	if id == 0 {
		return
	}
	c.Set("mailbox_account_id", id)
	var input mailbox.TemporaryCVVInput
	if !mailboxDecode(c, &input) {
		return
	}
	data, err := mailboxService().ProvideTemporaryCVV(c.Request.Context(), mailboxActor(c), id, input, accountType)
	input.CVV = ""
	if err != nil {
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
	accountType, ok := mailboxAccountType(c, input.AccountType)
	if !ok {
		return
	}
	input.AccountType = accountType
	if err := mailboxService().Assign(c.Request.Context(), mailboxActor(c), input); err != nil {
		mailboxFailure(c, err)
		return
	}
	mailboxSuccess(c, gin.H{})
}
func ListMailboxOperators(c *gin.Context) {
	if value := c.Query("include_stats"); len(c.QueryArray("include_stats")) > 1 || (value != "" && value != "false" && value != "true") {
		mailboxBadRequest(c, "mailbox_invalid_work_query")
		return
	}
	if c.Query("include_stats") == "true" {
		listMailboxOperatorsWithStats(c)
		return
	}
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
	accountType, ok := mailboxAccountType(c, c.Query("account_type"))
	if !ok {
		return
	}
	id := mailboxID(c)
	if id == 0 {
		return
	}
	var input mailbox.ReviewInput
	if !mailboxDecode(c, &input) {
		return
	}
	if err := mailboxService().Review(c.Request.Context(), mailboxActor(c), id, input, accountType); err != nil {
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
	query := model.DB.Model(&model.MailboxAudit{}).Where("account_type = ?", q.AccountType)
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
