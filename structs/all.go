package structs

// JobState ...
type JobState map[string]int

// NomadState ...
type NomadState struct {
	Info map[string]string
	Jobs map[string]JobInfo
}

type JobInfo struct {
	Project      string
	Description  string             `yaml:"description,omitempty"`
	CronitorID   string             `yaml:"cronitor_id,omitempty"`
	Type         string             `yaml:"type"`
	Cron         string             `yaml:"cron,omitempty"`
	InternalType string             `yaml:"internal_type"`
	Groups       map[string]Details `yaml:"groups"`
}

type Details struct {
	Count            int      `yaml:"count"`
	MaintenanceCount int      `yaml:"maintenance_count"`
	Services         []string `yaml:"services,omitempty"`
}
