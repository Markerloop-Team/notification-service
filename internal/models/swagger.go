package models

// Swagger response wrappers

type ErrorResponseSwagger struct {
	Success bool   `json:"success" example:"false"`
	Message string `json:"message" example:"Ошибка"`
	Error   string `json:"error" example:"bad_request"`
}

type EmailSuccessResponse struct {
	Success bool           `json:"success" example:"true"`
	Message string         `json:"message"`
	Data    *EmailResponse `json:"data"`
}
