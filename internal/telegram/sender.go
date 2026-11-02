package telegram

import "context"

// DeployPayload — расширенный набор полей, проксируемый из
// notification-service в markerloop-bot. Дублирует
// `models.DeployNotificationRequest` без зависимости пакета `telegram`
// от `models`, чтобы интерфейс оставался без cycles.
type DeployPayload struct {
	App          string `json:"app"`
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

// TelegramSender интерфейс для отправки Telegram уведомлений
type TelegramSender interface {
	// SendMessage отправляет сообщение конкретному пользователю через бот
	SendMessage(ctx context.Context, userID, title, message string) error
	// SendDeployNotificationPayload пересылает rich-уведомление о
	// деплое в бот. Старая сигнатура `SendDeployNotification(app,
	// status, revision)` остаётся для обратной совместимости — она
	// просто оборачивает аргументы в DeployPayload.
	SendDeployNotificationPayload(ctx context.Context, payload DeployPayload) error
}
