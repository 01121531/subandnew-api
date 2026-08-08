package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/01121531/HUICHUAN-AI/common"
	"github.com/01121531/HUICHUAN-AI/dto"
	"gorm.io/gorm"
)

const UserNameMaxLength = 20

// User contains only control-plane identity and authorization data. Legacy
// commercial columns may remain in existing databases but are not mapped.
type User struct {
	Id               int                        `json:"id"`
	Username         string                     `json:"username" gorm:"unique;index" validate:"max=20"`
	Password         string                     `json:"password" gorm:"not null" validate:"min=8,max=20"`
	OriginalPassword string                     `json:"original_password" gorm:"-:all"`
	DisplayName      string                     `json:"display_name" gorm:"index" validate:"max=20"`
	Role             int                        `json:"role" gorm:"type:int;default:1"`
	Status           int                        `json:"status" gorm:"type:int;default:1"`
	Email            string                     `json:"email" gorm:"index" validate:"max=50"`
	GitHubId         string                     `json:"github_id" gorm:"column:github_id;index"`
	DiscordId        string                     `json:"discord_id" gorm:"column:discord_id;index"`
	OidcId           string                     `json:"oidc_id" gorm:"column:oidc_id;index"`
	WeChatId         string                     `json:"wechat_id" gorm:"column:wechat_id;index"`
	TelegramId       string                     `json:"telegram_id" gorm:"column:telegram_id;index"`
	VerificationCode string                     `json:"verification_code" gorm:"-:all"`
	DeletedAt        gorm.DeletedAt             `gorm:"index"`
	LinuxDOId        string                     `json:"linux_do_id" gorm:"column:linux_do_id;index"`
	Setting          string                     `json:"setting" gorm:"type:text;column:setting"`
	Remark           string                     `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	CreatedAt        int64                      `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	LastLoginAt      int64                      `json:"last_login_at" gorm:"default:0;column:last_login_at"`
	AdminPermissions map[string]map[string]bool `json:"admin_permissions,omitempty" gorm:"-:all"`
}

func (user *User) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		if err := common.Unmarshal([]byte(user.Setting), &setting); err != nil {
			common.SysLog("failed to unmarshal user setting: " + err.Error())
		}
	}
	return setting
}

func (user *User) SetSetting(setting dto.UserSetting) {
	data, err := common.Marshal(setting)
	if err != nil {
		common.SysLog("failed to marshal user setting: " + err.Error())
		return
	}
	user.Setting = string(data)
}

func UpdateUserSetting(userID int, setting dto.UserSetting) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	data, err := common.Marshal(setting)
	if err != nil {
		return err
	}
	return DB.Model(&User{}).Where("id = ?", userID).Update("setting", string(data)).Error
}

func CheckUserExistOrDeleted(username, email string) (bool, error) {
	email = NormalizeEmail(email)
	query := DB.Unscoped().Model(&User{}).Where("username = ?", username)
	if email != "" {
		query = DB.Unscoped().Model(&User{}).Where("username = ? OR LOWER(email) = ?", username, email)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func emailQuery(tx *gorm.DB, email string) *gorm.DB {
	if tx == nil {
		tx = DB
	}
	return tx.Unscoped().Model(&User{}).Where("LOWER(email) = ?", NormalizeEmail(email))
}

func CountUsersByEmail(email string) (int64, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return 0, nil
	}
	var count int64
	err := emailQuery(DB, email).Count(&count).Error
	return count, err
}

func IsEmailAvailable(email string, excludeUserID int) (bool, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return true, nil
	}
	query := emailQuery(DB, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

func EnsureEmailAvailable(email string, excludeUserID int) error {
	available, err := IsEmailAvailable(email, excludeUserID)
	if err != nil {
		return err
	}
	if !available {
		return ErrEmailAlreadyTaken
	}
	return nil
}

func withNormalizedEmailLock(tx *gorm.DB, email string, fn func(*gorm.DB) error) error {
	email = NormalizeEmail(email)
	if email == "" {
		return fn(tx)
	}
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", email).Error; err != nil {
			return err
		}
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		var ids []int
		if err := tx.Raw("SELECT id FROM users WHERE email = ? FOR UPDATE", email).Scan(&ids).Error; err != nil {
			return err
		}
	}
	return fn(tx)
}

func GetAllUsers(pageInfo *common.PageInfo) ([]*User, int64, error) {
	var users []*User
	var total int64
	query := DB.Unscoped().Model(&User{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password").Find(&users).Error
	return users, total, err
}

func SearchUsers(keyword string, role, status *int, startIdx, count int) ([]*User, int64, error) {
	query := DB.Unscoped().Model(&User{})
	like := "%" + keyword + "%"
	condition := "username LIKE ? OR email LIKE ? OR display_name LIKE ?"
	args := []interface{}{like, like, like}
	if id, err := strconv.Atoi(keyword); err == nil {
		condition = "id = ? OR " + condition
		args = append([]interface{}{id}, args...)
	}
	query = query.Where("("+condition+")", args...)
	if role != nil {
		query = query.Where("role = ?", *role)
	}
	if status != nil {
		if *status == -1 {
			query = query.Where("deleted_at IS NOT NULL")
		} else {
			query = query.Where("deleted_at IS NULL AND status = ?", *status)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []*User
	err := query.Omit("password").Order("id desc").Limit(count).Offset(startIdx).Find(&users).Error
	return users, total, err
}

func GetUserById(id int, selectPassword bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("user id is empty")
	}
	query := DB
	if !selectPassword {
		query = query.Omit("password")
	}
	user := &User{}
	err := query.First(user, "id = ?", id).Error
	return user, err
}

func DeleteUserById(id int) error {
	if id == 0 {
		return errors.New("user id is empty")
	}
	return (&User{Id: id}).Delete()
}

func HardDeleteUserById(id int) error {
	if id == 0 {
		return errors.New("user id is empty")
	}
	return (&User{Id: id}).HardDelete()
}

func (user *User) prepareForInsert(tx *gorm.DB) error {
	user.Email = NormalizeEmail(user.Email)
	if err := ensureEmailAvailableWithTx(tx, user.Email, 0); err != nil {
		return err
	}
	if user.Password == "" {
		return nil
	}
	hash, err := common.Password2Hash(user.Password)
	if err != nil {
		return err
	}
	user.Password = hash
	return nil
}

func ensureEmailAvailableWithTx(tx *gorm.DB, email string, excludeUserID int) error {
	email = NormalizeEmail(email)
	if email == "" {
		return nil
	}
	query := emailQuery(tx, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrEmailAlreadyTaken
	}
	return nil
}

func (user *User) Insert(_ int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return user.InsertWithTx(tx, 0)
	})
}

func (user *User) InsertWithTx(tx *gorm.DB, _ int) error {
	return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
		if err := user.prepareForInsert(tx); err != nil {
			return err
		}
		if user.Setting == "" {
			user.SetSetting(dto.UserSetting{})
		}
		return tx.Create(user).Error
	})
}

func (user *User) FinishInsert(_ int) {}

func (user *User) FinalizeOAuthUserCreation(_ int) {}

func BindEmailToUser(user *User, email string) error {
	email = NormalizeEmail(email)
	return DB.Transaction(func(tx *gorm.DB) error {
		return withNormalizedEmailLock(tx, email, func(tx *gorm.DB) error {
			if err := ensureEmailAvailableWithTx(tx, email, user.Id); err != nil {
				return err
			}
			user.Email = email
			return tx.Model(&User{}).Where("id = ?", user.Id).Update("email", email).Error
		})
	})
}

func (user *User) Update(updatePassword bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return user.UpdateWithTx(tx, updatePassword)
	})
}

func (user *User) UpdateWithTx(tx *gorm.DB, updatePassword bool) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	if updatePassword {
		hash, err := common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
		user.Password = hash
	}
	user.Email = NormalizeEmail(user.Email)
	return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
		if err := ensureEmailAvailableWithTx(tx, user.Email, user.Id); err != nil {
			return err
		}
		updates := *user
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(user, user.Id).Error
	})
}

func (user *User) Edit(updatePassword bool) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		return user.EditWithTx(tx, updatePassword)
	})
}

func (user *User) EditWithTx(tx *gorm.DB, updatePassword bool) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	updates := map[string]interface{}{
		"username":     strings.TrimSpace(user.Username),
		"display_name": user.DisplayName,
		"remark":       user.Remark,
	}
	if updatePassword {
		hash, err := common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
		updates["password"] = hash
	}
	if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(updates).Error; err != nil {
		return err
	}
	return tx.First(user, user.Id).Error
}

func (user *User) ClearBinding(bindingType string) error {
	columns := map[string]string{
		"email": "email", "github": "github_id", "discord": "discord_id",
		"oidc": "oidc_id", "wechat": "wechat_id", "telegram": "telegram_id", "linuxdo": "linux_do_id",
	}
	column, ok := columns[bindingType]
	if user.Id == 0 || !ok {
		return errors.New("invalid user binding")
	}
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Update(column, "").Error; err != nil {
		return err
	}
	return DB.First(user, user.Id).Error
}

func (user *User) Delete() error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	return DB.Delete(user).Error
}

func (user *User) HardDelete() error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := deleteUserAuthenticationData(tx, user.Id); err != nil {
			return err
		}
		return tx.Unscoped().Delete(user).Error
	})
}

func deleteUserAuthenticationData(tx *gorm.DB, userID int) error {
	for _, data := range []any{&TwoFABackupCode{}, &TwoFA{}, &PasskeyCredential{}} {
		if err := tx.Unscoped().Where("user_id = ?", userID).Delete(data).Error; err != nil {
			return err
		}
	}
	return deleteUserOAuthBindingsByUserId(tx, userID)
}

func (user *User) ValidateAndFill() error {
	password := user.Password
	username := strings.TrimSpace(user.Username)
	if username == "" || password == "" {
		return ErrUserEmptyCredentials
	}
	if err := DB.Where("username = ? OR LOWER(email) = ?", username, NormalizeEmail(username)).First(user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if user.Password == "" || !common.ValidatePasswordAndHash(password, user.Password) || user.Status != common.UserStatusEnabled {
		return ErrInvalidCredentials
	}
	return nil
}

func (user *User) FillUserById() error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	return DB.First(user, user.Id).Error
}

func (user *User) FillUserByEmail() error {
	if NormalizeEmail(user.Email) == "" {
		return errors.New("email is empty")
	}
	return DB.Where("LOWER(email) = ?", NormalizeEmail(user.Email)).First(user).Error
}

func (user *User) fillBy(column, value string) error {
	if value == "" {
		return errors.New(column + " is empty")
	}
	return DB.Where(column+" = ?", value).First(user).Error
}

func (user *User) FillUserByGitHubId() error   { return user.fillBy("github_id", user.GitHubId) }
func (user *User) FillUserByDiscordId() error  { return user.fillBy("discord_id", user.DiscordId) }
func (user *User) FillUserByOidcId() error     { return user.fillBy("oidc_id", user.OidcId) }
func (user *User) FillUserByWeChatId() error   { return user.fillBy("wechat_id", user.WeChatId) }
func (user *User) FillUserByTelegramId() error { return user.fillBy("telegram_id", user.TelegramId) }
func (user *User) FillUserByLinuxDOId() error  { return user.fillBy("linux_do_id", user.LinuxDOId) }

func (user *User) UpdateGitHubId(githubID string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	return DB.Model(user).Update("github_id", githubID).Error
}

func IsEmailAlreadyTaken(email string) bool {
	count, err := CountUsersByEmail(email)
	return err == nil && count > 0
}

func GetUniqueUserByEmail(email string) (*User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, ErrEmailNotFound
	}
	var users []User
	if err := DB.Where("LOWER(email) = ?", email).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, ErrEmailNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, ErrEmailAmbiguous
	}
}

func isIdentityTaken(column, value string, includeDeleted bool) bool {
	if value == "" {
		return false
	}
	query := DB.Model(&User{})
	if includeDeleted {
		query = query.Unscoped()
	}
	var count int64
	return query.Where(column+" = ?", value).Count(&count).Error == nil && count > 0
}

func IsWeChatIdAlreadyTaken(id string) bool   { return isIdentityTaken("wechat_id", id, true) }
func IsGitHubIdAlreadyTaken(id string) bool   { return isIdentityTaken("github_id", id, true) }
func IsDiscordIdAlreadyTaken(id string) bool  { return isIdentityTaken("discord_id", id, true) }
func IsOidcIdAlreadyTaken(id string) bool     { return isIdentityTaken("oidc_id", id, false) }
func IsTelegramIdAlreadyTaken(id string) bool { return isIdentityTaken("telegram_id", id, true) }
func IsLinuxDOIdAlreadyTaken(id string) bool  { return isIdentityTaken("linux_do_id", id, true) }

func ResetUserPasswordByEmail(email, password string) error {
	if NormalizeEmail(email) == "" || password == "" {
		return errors.New("email or password is empty")
	}
	user, err := GetUniqueUserByEmail(email)
	if err != nil {
		return err
	}
	hash, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	return DB.Model(&User{}).Where("id = ?", user.Id).Update("password", hash).Error
}

func IsAdmin(userID int) bool {
	var role int
	return userID > 0 && DB.Model(&User{}).Select("role").Where("id = ?", userID).Scan(&role).Error == nil && role >= common.RoleAdminUser
}

func UpdateUserLastLoginAt(id int) {
	if err := DB.Model(&User{}).Where("id = ?", id).Update("last_login_at", common.GetTimestamp()).Error; err != nil {
		common.SysLog("failed to update user last login time: " + err.Error())
	}
}

func GetUserLanguage(userID int) string {
	var setting string
	if err := DB.Model(&User{}).Select("setting").Where("id = ?", userID).Scan(&setting).Error; err != nil {
		return ""
	}
	user := User{Setting: setting}
	return user.GetSetting().Language
}

func InvalidateUserCache(userID int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(fmt.Sprintf("user:%d", userID))
}

func RootUserExists() bool {
	var count int64
	return DB.Model(&User{}).Where("role = ?", common.RoleRootUser).Count(&count).Error == nil && count > 0
}
