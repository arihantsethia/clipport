package terminal

type SessionKind string

const (
	SessionUnknown SessionKind = ""
	SessionLocal   SessionKind = "local"
	SessionRemote  SessionKind = "remote"
)

type Session struct {
	Terminal     string
	SessionKey   string
	DetectedHost string
	RawTitle     string
	Kind         SessionKind
}

type ActiveSessionProvider interface {
	ActiveSession() (Session, error)
}
