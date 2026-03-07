package handlers

// NavUser es el struct inyectado en todas las templates como .User.
// Definido aquí para que sea accesible desde me.go, shared.go y server.go via handlers.NavUser
type NavUser struct {
	ID          string
	Email       string
	DisplayName string
	AvatarURL   string
	Role        string
	MFAEnabled  bool
	Initials    string
}
