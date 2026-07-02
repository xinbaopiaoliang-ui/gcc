package systemstats

type Snapshot struct {
	CPU         CPUSnapshot         `json:"cpu"`
	Memory      MemorySnapshot      `json:"memory"`
	Disk        DiskSnapshot        `json:"disk"`
	Network     NetworkSnapshot     `json:"network"`
	LoadAverage LoadAverageSnapshot `json:"load_average"`
}

type CPUSnapshot struct {
	Cores   int     `json:"cores"`
	Percent float64 `json:"percent"`
}

type MemorySnapshot struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

type DiskSnapshot struct {
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

type NetworkSnapshot struct {
	RXBytes       uint64  `json:"rx_bytes"`
	TXBytes       uint64  `json:"tx_bytes"`
	RXRateBytes   float64 `json:"rx_rate_bytes_per_second"`
	TXRateBytes   float64 `json:"tx_rate_bytes_per_second"`
	SampleSeconds float64 `json:"sample_seconds"`
}

type LoadAverageSnapshot struct {
	OneMinute      float64 `json:"one_minute"`
	FiveMinutes    float64 `json:"five_minutes"`
	FifteenMinutes float64 `json:"fifteen_minutes"`
}

func (s Snapshot) HasData() bool {
	return s.CPU.Cores > 0 ||
		s.Memory.TotalBytes > 0 ||
		s.Disk.TotalBytes > 0 ||
		s.Network.RXBytes > 0 ||
		s.Network.TXBytes > 0 ||
		s.LoadAverage.OneMinute > 0 ||
		s.LoadAverage.FiveMinutes > 0 ||
		s.LoadAverage.FifteenMinutes > 0
}
