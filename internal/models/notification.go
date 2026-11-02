package models

// EmailRequest структура запроса на отправку email
type EmailRequest struct {
	To      string `json:"to" binding:"required,email"`
	Subject string `json:"subject" binding:"required"`
	Body    string `json:"body" binding:"required"`
}

// VerificationEmailRequest структура запроса на отправку email верификации
type VerificationEmailRequest struct {
	To   string `json:"to" binding:"required,email"`
	Code string `json:"code" binding:"required"`
}

// WelcomeEmailRequest структура запроса на отправку приветственного email
type WelcomeEmailRequest struct {
	To       string `json:"to" binding:"required,email"`
	UserName string `json:"user_name" binding:"required"`
}

// PasswordResetEmailRequest структура запроса на отправку email сброса пароля
type PasswordResetEmailRequest struct {
	To       string `json:"to" binding:"required,email"`
	ResetURL string `json:"reset_url" binding:"required"`
}

// WorkspaceInviteEmailRequest — письмо-приглашение в рабочее пространство
// (SDL-615, сервис-сервисная ручка /internal/email/workspace-invite).
// WorkspaceName и InviterName — пользовательский ввод: в письме они
// экранируются html/template, в тему письма не попадают. ExpiresAt — RFC3339.
// Locale — ru/en/kk, пусто или неизвестно → en.
type WorkspaceInviteEmailRequest struct {
	To            string `json:"to" binding:"required,email"`
	WorkspaceName string `json:"workspace_name" binding:"required"`
	InviterName   string `json:"inviter_name" binding:"required"`
	AcceptURL     string `json:"accept_url" binding:"required"`
	ExpiresAt     string `json:"expires_at" binding:"required"`
	Locale        string `json:"locale"`
}

// EmailResponse структура ответа на отправку email
type EmailResponse struct {
	Message string `json:"message"`
	To      string `json:"to"`
}

// TelegramSendRequest запрос на отправку Telegram сообщения пользователю
type TelegramSendRequest struct {
	UserID  string `json:"user_id" binding:"required"`
	Title   string `json:"title" binding:"required"`
	Message string `json:"message" binding:"required"`
}

// TelegramSendResponse ответ на отправку Telegram сообщения
type TelegramSendResponse struct {
	Message string `json:"message"`
	UserID  string `json:"user_id"`
}

// DeployNotificationRequest запрос от ArgoCD Notifications webhook,
// проксируемый дальше в markerloop-bot. App обязателен; всё остальное
// опционально — старые шаблоны (только app/status/revision) продолжают
// работать, новые поля добавляются по мере того, как dev-infra
// (`argocd-notifications-cm`) начинает их слать.
//
// EventType дискриминирует ArgoCD trigger id и используется ботом
// для подбора иконки/заголовка:
//
//	on-deployed         🚀 Deployed
//	on-sync-running     🟡 Sync running
//	on-sync-failed      🔴 Sync failed
//	on-health-degraded  ⚠️  Health degraded
type DeployNotificationRequest struct {
	App          string `json:"app" binding:"required"`
	Status       string `json:"status,omitempty"`
	Revision     string `json:"revision,omitempty"`
	EventType    string `json:"event_type,omitempty"`
	RevisionURL  string `json:"revision_url,omitempty"`
	ArgocdURL    string `json:"argocd_url,omitempty"`
	RepoURL      string `json:"repo_url,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	Message      string `json:"message,omitempty"`
	HealthStatus string `json:"health_status,omitempty"`
	SyncStatus   string `json:"sync_status,omitempty"`
}

// DeployNotificationResponse ответ на deploy notification
type DeployNotificationResponse struct {
	Message string `json:"message"`
	App     string `json:"app"`
}
