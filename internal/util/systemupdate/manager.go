package systemupdate

type PackageStatus struct {
	Name             string `json:"name"`
	InstalledVersion string `json:"installedVersion"`
	AvailableVersion string `json:"availableVersion"`
	Installed        bool   `json:"installed"`
	Required         bool   `json:"required"`
	Kernel           bool   `json:"kernel"`
	UpdateAvailable  bool   `json:"updateAvailable"`
}

type KernelStatus struct {
	RunningVersion   string   `json:"runningVersion"`
	UpdateAvailable  bool     `json:"updateAvailable"`
	AvailableVersion string   `json:"availableVersion"`
	PackageNames     []string `json:"packageNames"`
	RebootRequired   bool     `json:"rebootRequired"`
}

type Status struct {
	Distribution     string          `json:"distribution"`
	Version          string          `json:"version"`
	PackageManager   string          `json:"packageManager"`
	Supported        bool            `json:"supported"`
	RunningAsRoot    bool            `json:"runningAsRoot"`
	Packages         []PackageStatus `json:"packages"`
	Kernel           KernelStatus    `json:"kernel"`
	UpdatesAvailable bool            `json:"updatesAvailable"`
	MissingPackages  bool            `json:"missingPackages"`
	CanUpdate        bool            `json:"canUpdate"`
	AllPackages      bool            `json:"allPackages"`
	Notes            []string        `json:"notes"`
}

type UpdateResult struct {
	Updated        bool   `json:"updated"`
	RebootRequired bool   `json:"rebootRequired"`
	Output         string `json:"output"`
	Error          string `json:"error"`
}
