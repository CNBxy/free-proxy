package domain

// The settings the web console owns. Everything an operator has to decide is
// here — who they are, where the service listens, and who may reach it — and
// nothing else is: the values that only tune how the program behaves live as
// constants in internal/config.

type AdminSettings struct {
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	// Password is the recoverable copy of the admin password, kept so
	// `free-proxy credentials` can print it. Never serialized to the API.
	Password          string `json:"-"`
	SecretPath        string `json:"secret_path"`
	WebPort           int    `json:"web_port"`
	WebExternalAccess bool   `json:"web_external_access"`
	PasswordSet       bool   `json:"password_set"`
}

type ProxyServiceSettings struct {
	Enabled        bool   `json:"enabled"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	PasswordHash   string `json:"-"`
	PasswordSet    bool   `json:"password_set"`
	ExternalAccess bool   `json:"external_access"`
}

type AppSettings struct {
	Admin AdminSettings        `json:"admin"`
	Proxy ProxyServiceSettings `json:"proxy"`
}
