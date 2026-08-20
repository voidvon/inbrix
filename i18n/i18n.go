// Package i18n contains Inbrix AI's user-interface translations and locale-aware
// formatting helpers. Mail content is never passed through this package.
package i18n

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	LocaleEnglish = "en"
	LocaleZhCN    = "zh-CN"
)

type LocaleOption struct {
	Code string
	Name string
}

var SupportedLocales = []LocaleOption{
	{Code: LocaleEnglish, Name: "English"},
	{Code: LocaleZhCN, Name: "简体中文"},
}

// zhCN contains only translations whose English source text is used as the
// template/JavaScript key. English deliberately falls back to the key so a
// missing translation cannot make a control disappear.
var zhCN = map[string]string{
	"Toggle sidebar":                   "展开/收起侧栏",
	"Inbrix AI - go to inbox":          "Inbrix AI - 前往收件箱",
	"Search mail":                      "搜索邮件",
	"Settings":                         "设置",
	"Sign out":                         "退出登录",
	"Language":                         "语言",
	"Switch mailbox":                   "切换邮箱",
	"Current mailbox":                  "当前邮箱",
	"Manage mailboxes":                 "管理邮箱",
	"English":                          "English",
	"Email address":                    "邮箱地址",
	"Password":                         "密码",
	"Remember me for 30 days":          "30 天内记住我",
	"Signing in…":                      "正在登录…",
	"Sign in":                          "登录",
	"or":                               "或",
	"Continue with OAuth2":             "使用 OAuth2 继续",
	"Requires IMAP server credentials": "需要 IMAP 服务器凭据",
	"Open-source licences":             "开源许可证",
	"Error":                            "错误",
	"Page not found":                   "页面未找到",
	"Authentication required":          "需要身份验证",
	"Access forbidden":                 "禁止访问",
	"Server error":                     "服务器错误",
	"Something went wrong":             "出了点问题",
	"Sorry, we couldn't find the page you're looking for.":  "抱歉，找不到你要访问的页面。",
	"Please log in to access this page.":                    "请登录后访问此页面。",
	"You don't have permission to access this page.":        "你没有访问此页面的权限。",
	"An unexpected error occurred. Please try again later.": "发生意外错误，请稍后重试。",
	"An error occurred while processing your request.":      "处理请求时发生错误。",
	"Go to login":                       "前往登录",
	"Go back":                           "返回上一页",
	"Home":                              "首页",
	"Technical Details":                 "技术详情",
	"Status":                            "状态",
	"Path":                              "路径",
	"Stack":                             "堆栈",
	"Compose":                           "写邮件",
	"Calendar":                          "日历",
	"Folders":                           "文件夹",
	"Inbox":                             "收件箱",
	"Conversations":                     "对话",
	"Conversation":                      "对话",
	"Select a conversation":             "选择一个对话",
	"No conversations":                  "暂无对话",
	"No conversations matched":          "没有匹配的对话",
	"Refresh conversations":             "刷新对话",
	"conversations":                     "个对话",
	"messages":                          "条消息",
	"Me":                                "我",
	"Sent":                              "已发送",
	"Sent Items":                        "已发送",
	"Sent Messages":                     "已发送",
	"Drafts":                            "草稿",
	"Deleted Items":                     "已删除",
	"Trash":                             "垃圾箱",
	"Junk Mail":                         "垃圾邮件",
	"Spam":                              "垃圾邮件",
	"Archive":                           "归档",
	"Unified Inbox":                     "统一收件箱",
	"Unified":                           "统一收件箱",
	"Switch to single-account inbox":    "切换到单账号收件箱",
	"Show all accounts in one list":     "在一个列表中显示所有账号",
	"Some accounts failed to load":      "部分账号加载失败",
	"Comfortable density":               "舒适密度",
	"Compact density":                   "紧凑密度",
	"Toggle list density":               "切换列表密度",
	"Reading pane layout":               "阅读窗格布局",
	"Pane on right":                     "窗格在右侧",
	"Pane on bottom":                    "窗格在底部",
	"No reading pane":                   "不显示阅读窗格",
	"selected":                          "已选择",
	"Mark unread":                       "标记为未读",
	"Delete selected":                   "删除选中邮件",
	"Delete":                            "删除",
	"Clear selection":                   "清除选择",
	"Select a message to read":          "选择一封邮件阅读",
	"Back":                              "返回",
	"All clear":                         "一切正常",
	"No messages in this folder.":       "此文件夹中没有邮件。",
	"No results":                        "没有结果",
	"No messages matched":               "没有匹配的邮件",
	"Select":                            "选择",
	"Back to inbox":                     "返回收件箱",
	"Has attachments":                   "包含附件",
	"Draft":                             "草稿",
	"New Message":                       "新邮件",
	"Restore":                           "还原",
	"Minimize":                          "最小化",
	"Close":                             "关闭",
	"Edit draft":                        "编辑草稿",
	"Edit Draft":                        "编辑草稿",
	"To":                                "收件人",
	"Recipients":                        "收件人",
	"CC/BCC":                            "抄送/密送",
	"CC":                                "抄送",
	"BCC":                               "密送",
	"Subj":                              "主题",
	"Subject":                           "主题",
	"Bold":                              "粗体",
	"Italic":                            "斜体",
	"Underline":                         "下划线",
	"Strikethrough":                     "删除线",
	"Ordered list":                      "有序列表",
	"Unordered list":                    "无序列表",
	"Link":                              "链接",
	"URL:":                              "URL：",
	"Remove formatting":                 "清除格式",
	"Write your message…":               "写下邮件内容…",
	"Message body (rich text)":          "邮件正文（富文本）",
	"Sending…":                          "正在发送…",
	"Send":                              "发送",
	"Saving…":                           "正在保存…",
	"Toggle rich text":                  "切换富文本",
	"Plain":                             "纯文本",
	"Rich":                              "富文本",
	"Save draft":                        "保存草稿",
	"Attach file":                       "附加文件",
	"Attach":                            "添加附件",
	"Discard":                           "丢弃",
	"Draft error":                       "草稿错误",
	"Draft is empty":                    "草稿为空",
	"Draft saved":                       "草稿已保存",
	"Could not save draft":              "无法保存草稿",
	"Failed to send email":              "邮件发送失败",
	"Failed to connect to mail server":  "连接邮件服务器失败",
	"To, subject and body are required": "收件人、主题和正文不能为空",
	"Email sent successfully!":          "邮件发送成功！",
	"Reply":                             "回复",
	"Reply all":                         "回复全部",
	"Reply All":                         "回复全部",
	"Show quoted message":               "显示引用内容",
	"Forward":                           "转发",
	"Archive (coming soon)":             "归档（即将推出）",
	"Mark as unread (u)":                "标记为未读（u）",
	"Flag":                              "标记",
	"Mark as junk (coming soon)":        "标记为垃圾邮件（即将推出）",
	"Junk":                              "垃圾邮件",
	"Print":                             "打印",
	"Delete (Del)":                      "删除（Del）",
	"Folder moves are not available in this build yet.": "此版本暂不支持移动文件夹。",
	"Calendar Invite": "日历邀请",
	"This email contains a calendar invitation.": "这封邮件包含日历邀请。",
	"Accept":             "接受",
	"Tentative":          "暂定",
	"Decline":            "拒绝",
	"Accepted":           "已接受",
	"Declined":           "已拒绝",
	"Error sending RSVP": "发送回复失败",
	"Response":           "回复结果",
	"Images in this message are blocked to protect your privacy.": "为保护隐私，邮件中的图片已被阻止加载。",
	"Display images":     "显示图片",
	"Email content":      "邮件内容",
	"Attachments":        "附件",
	"Forwarded message":  "转发的邮件",
	"From":               "发件人",
	"On":                 "于",
	"wrote":              "写道",
	"Push Notifications": "推送通知",
	"Receive notifications even when Inbrix AI is not open in a browser tab.":        "即使没有打开 Inbrix AI 浏览器标签页，也能接收通知。",
	"Requires permission from your browser.":                                         "需要获得浏览器授权。",
	"Enabling…":                                                                      "正在启用…",
	"Enable push notifications":                                                      "启用推送通知",
	"Disable push notifications":                                                     "停用推送通知",
	"Web Push is not supported by your browser or requires HTTPS.":                   "你的浏览器不支持 Web Push，或当前连接需要 HTTPS。",
	"Web Push is not supported in this browser.":                                     "此浏览器不支持 Web Push。",
	"Push notifications are enabled.":                                                "推送通知已启用。",
	"Push notifications are not enabled.":                                            "推送通知未启用。",
	"Push notifications are disabled.":                                               "推送通知已停用。",
	"Could not enable":                                                               "无法启用",
	"Additional Accounts":                                                            "其他账号",
	"Add extra IMAP/SMTP mail accounts. You can switch between them from this page.": "添加其他 IMAP/SMTP 邮箱账号，可在此页面切换账号。",
	"Account":                                "账号",
	"IMAP server":                            "IMAP 服务器",
	"Switch":                                 "切换",
	"Remove":                                 "移除",
	"No additional accounts. Add one below.": "没有其他账号，请在下方添加。",
	"Add account":                            "添加账号",
	"Label (optional)":                       "标签（可选）",
	"Badge colour (optional)":                "徽章颜色（可选）",
	"Advanced (server overrides)":            "高级设置（覆盖服务器配置）",
	"(use default)":                          "（使用默认值）",
	"IMAP port":                              "IMAP 端口",
	"SMTP server":                            "SMTP 服务器",
	"SMTP port":                              "SMTP 端口",
	"Multi-account support is disabled. Set": "多账号功能已禁用。请在",
	"to enable it.":                          "中设置后启用。",
	"About":                                  "关于",
	"inbrix is open source under the MIT licence. It includes third-party software whose licences require attribution:": "inbrix 使用 MIT 许可证开源，其中包含需要注明许可证的第三方软件：",
	"New Event":                             "新建事件",
	"Month view":                            "月视图",
	"Week view":                             "周视图",
	"Back to Mail":                          "返回邮件",
	"Today":                                 "今天",
	"Week":                                  "周",
	"This week":                             "本周",
	"Event title *":                         "事件标题 *",
	"All day":                               "全天",
	"Date":                                  "日期",
	"Start":                                 "开始",
	"End":                                   "结束",
	"Location":                              "地点",
	"Description":                           "描述",
	"Cancel":                                "取消",
	"Create":                                "创建",
	"New mail from":                         "新邮件，发件人：",
	"unknown":                               "未知发件人",
	"(no subject)":                          "（无主题）",
	"Push not supported in this browser":    "此浏览器不支持推送通知",
	"Malformed VAPID public key response":   "VAPID 公钥响应格式错误",
	"Server rejected subscription":          "服务器拒绝了订阅请求",
	"Notification permission denied":        "通知权限被拒绝",
	"OAuth2 is not enabled":                 "OAuth2 未启用",
	"Session error":                         "会话错误",
	"Failed to generate state":              "生成状态失败",
	"Failed to generate PKCE verifier":      "生成 PKCE 验证值失败",
	"Failed to save session":                "保存会话失败",
	"Email and password are required":       "邮箱地址和密码不能为空",
	"Invalid email format":                  "邮箱地址格式无效",
	"Invalid credentials or server error":   "凭据无效或服务器发生错误",
	"Server error occurred during setup":    "设置过程中发生服务器错误",
	"Failed to create authentication token": "创建身份验证令牌失败",
	"Failed to secure credentials":          "保护凭据失败",
	"Error during logout":                   "退出登录时发生错误",
	"Too many login attempts. Please wait and try again.":   "登录尝试次数过多，请稍后重试。",
	"Error loading folders":                                 "加载文件夹失败",
	"Error connecting to email server":                      "连接邮件服务器失败",
	"Error fetching emails":                                 "获取邮件失败",
	"Unauthorized":                                          "未授权",
	"Email ID required":                                     "需要邮件 ID",
	"Attachment ID required":                                "需要附件 ID",
	"Invalid attachment ID":                                 "附件 ID 无效",
	"Error fetching attachment":                             "获取附件失败",
	"Attachment exceeds maximum allowed size":               "附件超过允许的大小限制",
	"Invalid token":                                         "令牌无效",
	"Event UID required":                                    "需要事件 UID",
	"Calendar not available":                                "日历不可用",
	"Failed to list events":                                 "列出事件失败",
	"Event not found":                                       "事件未找到",
	"Request failed: ":                                      "请求失败：",
	"Connection timed out. Check the IMAP server and port.": "连接超时，请检查 IMAP 服务器和端口。",
	"Unknown error":                                         "未知错误",
	"Checking…":                                             "正在检查…",
	"Adding…":                                               "正在添加…",
	"Application login":                                     "应用账号",
	"Application password":                                  "应用密码",
	"Your inbrix account keeps multiple mailboxes together.": "你的 inbrix 账号可以集中管理多个邮箱。",
	"Create an application account":                          "创建应用账号",
	"Sign in with a mailbox directly":                        "直接使用邮箱登录",
	"Sign in with a inbrix account":                          "使用 inbrix 账号登录",
	"Login":                                                  "登录账号",
	"Display name (optional)":                                "显示名称（可选）",
	"Confirm password":                                       "确认密码",
	"Creating…":                                              "正在创建…",
	"Create account":                                         "创建账号",
	"Already have an application account? Sign in":           "已有应用账号？立即登录",
	"Invalid application login":                              "应用账号或密码无效",
	"Login and matching password of at least 8 characters are required": "请输入账号，并设置至少 8 位且两次一致的密码",
	"An application account with this login already exists":             "该应用账号已存在",
	"Could not create application account":                              "无法创建应用账号",
	"Failed to load mail accounts":                                      "加载邮箱账号失败",
	"Failed to save application account":                                "保存应用账号失败",
}

// Normalize accepts browser language tags and the values used by the switcher.
func Normalize(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if strings.HasPrefix(raw, "zh") {
		return LocaleZhCN
	}
	return LocaleEnglish
}

// Detect prefers the explicit cookie and falls back to Accept-Language.
func Detect(cookie, acceptLanguage string) string {
	if strings.TrimSpace(cookie) != "" {
		return Normalize(cookie)
	}

	bestLocale := LocaleEnglish
	bestQuality := -1.0
	for _, part := range strings.Split(acceptLanguage, ",") {
		segments := strings.Split(part, ";")
		language := strings.ToLower(strings.TrimSpace(segments[0]))
		locale := LocaleEnglish
		if strings.HasPrefix(language, "zh") {
			locale = LocaleZhCN
		} else if !strings.HasPrefix(language, "en") {
			continue
		}

		quality := 1.0
		for _, parameter := range segments[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil {
				quality = 0
			} else {
				quality = parsed
			}
			break
		}

		if quality > bestQuality && quality > 0 {
			bestLocale = locale
			bestQuality = quality
		}
	}
	return bestLocale
}

// Dictionary returns an isolated dictionary for the selected locale. The map
// is safe to pass to templates and JSON encoding without exposing package
// state to callers.
func Dictionary(locale string) map[string]string {
	result := make(map[string]string, len(zhCN))
	if Normalize(locale) == LocaleZhCN {
		for key, value := range zhCN {
			result[key] = value
		}
	}
	return result
}

func Translate(locale, key string) string {
	if Normalize(locale) == LocaleZhCN {
		if value, ok := zhCN[key]; ok {
			return value
		}
	}
	return key
}

func TranslateDictionary(dictionary map[string]string, key string) string {
	if value, ok := dictionary[key]; ok {
		return value
	}
	return key
}

func FolderLabel(locale, name string) string {
	labels := map[string]string{
		"INBOX":         "Inbox",
		"Sent Items":    "Sent Items",
		"Sent":          "Sent",
		"Sent Messages": "Sent Messages",
		"Drafts":        "Drafts",
		"Deleted Items": "Deleted Items",
		"Trash":         "Trash",
		"Junk Mail":     "Junk Mail",
		"Spam":          "Spam",
		"Archive":       "Archive",
	}
	if english, ok := labels[name]; ok {
		return Translate(locale, english)
	}
	return name
}

func FormatDate(locale string, value time.Time) string {
	if Normalize(locale) == LocaleZhCN {
		return fmt.Sprintf("%d年%d月%d日 %02d:%02d", value.Year(), value.Month(), value.Day(), value.Hour(), value.Minute())
	}
	return value.Format("Jan 02, 2006 15:04")
}

func FormatMonthTitle(locale string, month int, year int) string {
	if Normalize(locale) == LocaleZhCN {
		return fmt.Sprintf("%d年%d月", year, month)
	}
	return fmt.Sprintf("%s %d", time.Month(month).String(), year)
}

func FormatWeekTitle(locale string, monday time.Time) string {
	if Normalize(locale) == LocaleZhCN {
		return fmt.Sprintf("%d年%d月%d日所在周", monday.Year(), monday.Month(), monday.Day())
	}
	return "Week of " + monday.Format("Jan 2, 2006")
}

func FormatCalendarDate(locale string, value time.Time) string {
	if Normalize(locale) == LocaleZhCN {
		return fmt.Sprintf("%d年%d月%d日 %s", value.Year(), value.Month(), value.Day(), WeekdayName(locale, value.Weekday()))
	}
	return value.Format("Monday, January 2, 2006")
}

func WeekdayName(locale string, value interface{}) string {
	var weekday time.Weekday
	switch typed := value.(type) {
	case time.Weekday:
		weekday = typed
	case int:
		weekday = time.Weekday(typed)
	case int64:
		weekday = time.Weekday(typed)
	default:
		weekday = time.Monday
	}
	if Normalize(locale) == LocaleZhCN {
		if weekday < time.Sunday || weekday > time.Saturday {
			weekday = time.Monday
		}
		return []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}[weekday]
	}
	if weekday < time.Sunday || weekday > time.Saturday {
		weekday = time.Monday
	}
	return weekday.String()[:3]
}
