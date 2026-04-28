package terminal

type Session struct {
	Terminal     string
	SessionKey   string
	DetectedHost string
	RawTitle     string
}

type ActiveSessionProvider interface {
	ActiveSession() (Session, error)
}
