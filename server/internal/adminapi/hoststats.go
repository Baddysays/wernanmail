package adminapi

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type memStats struct {
	TotalBytes     uint64  `json:"totalBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	UsedPercent    float64 `json:"usedPercent"`
}

type diskStats struct {
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"totalBytes"`
	UsedBytes   uint64  `json:"usedBytes"`
	FreeBytes   uint64  `json:"freeBytes"`
	UsedPercent float64 `json:"usedPercent"`
}

type procStat struct {
	Name       string  `json:"name"`
	RSSBytes   uint64  `json:"rssBytes"`
	CPUPercent float64 `json:"cpuPercent,omitempty"`
	PID        int     `json:"pid,omitempty"`
}

type hostSnapshot struct {
	Mem            memStats   `json:"mem"`
	Disk           diskStats  `json:"disk"`
	Load1          float64    `json:"load1"`
	Load5          float64    `json:"load5"`
	Load15         float64    `json:"load15"`
	CPUs           int        `json:"cpus"`
	MailRSS        uint64     `json:"mailRssBytes"`
	MailCPUPercent float64    `json:"mailCpuPercent"`
	DataBytes      uint64     `json:"dataBytes"`
	BinBytes       uint64     `json:"binBytes"`
	Processes      []procStat `json:"processes"`
	UptimeSec      uint64     `json:"uptimeSec"`
}

func collectHostStats(dataDir string) hostSnapshot {
	dataDir = firstNonEmpty(dataDir, "/opt/wernanmail/data")
	out := hostSnapshot{
		CPUs: runtime.NumCPU(),
		Disk: diskUsage(dataDir),
	}
	out.Mem = readMemInfo()
	out.Load1, out.Load5, out.Load15 = readLoadAvg()
	out.UptimeSec = readUptime()
	out.DataBytes = dirSize(dataDir)
	root := filepath.Clean(filepath.Dir(dataDir))
	out.BinBytes = dirSize(filepath.Join(root, "bin"))

	procs, cpu := mailProcessStats()
	out.Processes = procs
	out.MailCPUPercent = cpu
	for _, p := range out.Processes {
		out.MailRSS += p.RSSBytes
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "/"
}

func readMemInfo() memStats {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return memStats{TotalBytes: m.Sys, UsedBytes: m.Alloc, AvailableBytes: m.Sys - m.Alloc}
	}
	defer f.Close()
	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		n, _ := strconv.ParseUint(fields[1], 10, 64)
		vals[strings.TrimSuffix(fields[0], ":")] = n * 1024
	}
	total := vals["MemTotal"]
	avail := vals["MemAvailable"]
	if avail == 0 {
		avail = vals["MemFree"] + vals["Buffers"] + vals["Cached"]
	}
	used := uint64(0)
	if total > avail {
		used = total - avail
	}
	pct := 0.0
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return memStats{TotalBytes: total, AvailableBytes: avail, UsedBytes: used, UsedPercent: pct}
}

func readLoadAvg() (float64, float64, float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	parts := strings.Fields(string(b))
	if len(parts) < 3 {
		return 0, 0, 0
	}
	a, _ := strconv.ParseFloat(parts[0], 64)
	b5, _ := strconv.ParseFloat(parts[1], 64)
	c, _ := strconv.ParseFloat(parts[2], 64)
	return a, b5, c
}

func readUptime() uint64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) < 1 {
		return 0
	}
	f, _ := strconv.ParseFloat(parts[0], 64)
	return uint64(f)
}

func dirSize(root string) uint64 {
	root = strings.TrimSpace(root)
	if root == "" {
		return 0
	}
	var total uint64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

func mailProcessStats() ([]procStat, float64) {
	found := findMailPIDs()
	if len(found) == 0 {
		return nil, 0
	}
	total1 := readTotalCPU()
	ticks1 := map[int]uint64{}
	for _, p := range found {
		if t, ok := readProcCPU(p.pid); ok {
			ticks1[p.pid] = t
		}
	}
	time.Sleep(120 * time.Millisecond)
	total2 := readTotalCPU()
	deltaTotal := total2 - total1
	out := make([]procStat, 0, len(found))
	var sumCPU float64
	for _, p := range found {
		rss := readStatusRSS(filepath.Join("/proc", strconv.Itoa(p.pid), "status"))
		if rss == 0 {
			continue
		}
		var cpu float64
		if t2, ok := readProcCPU(p.pid); ok && deltaTotal > 0 {
			if t1, ok1 := ticks1[p.pid]; ok1 && t2 >= t1 {
				cpu = 100 * float64(t2-t1) / float64(deltaTotal)
			}
		}
		sumCPU += cpu
		out = append(out, procStat{Name: p.name, RSSBytes: rss, CPUPercent: cpu, PID: p.pid})
	}
	return out, sumCPU
}

type mailPID struct {
	name string
	pid  int
}

func findMailPIDs() []mailPID {
	names := []string{"mta", "imapd", "worker", "admin", "api"}
	want := map[string]struct{}{}
	for _, n := range names {
		want[n] = struct{}{}
	}
	out := make([]mailPID, 0, len(names))
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		exeb, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue
		}
		// Ignore replaced-but-still-running binaries from prior deploys ("… (deleted)").
		if strings.Contains(exeb, "(deleted)") {
			continue
		}
		base := filepath.Base(strings.TrimSpace(exeb))
		if _, ok := want[base]; !ok {
			continue
		}
		// Prefer our install tree when present.
		if !strings.Contains(exeb, "wernanmail") && !strings.HasSuffix(exeb, "/"+base) {
			continue
		}
		out = append(out, mailPID{name: base, pid: pid})
	}
	return out
}

func readStatusRSS(path string) uint64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				n, _ := strconv.ParseUint(fields[1], 10, 64)
				return n * 1024
			}
		}
	}
	return 0
}

func readProcCPU(pid int) (uint64, bool) {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	s := string(b)
	idx := strings.LastIndex(s, ")")
	if idx < 0 || idx+2 >= len(s) {
		return 0, false
	}
	fields := strings.Fields(s[idx+2:])
	// after ')': state ppid ... utime(14) stime(15) → indexes 11,12 in this slice (field 3 = index 0)
	if len(fields) < 13 {
		return 0, false
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	return utime + stime, true
}

func readTotalCPU() uint64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 2 || fields[0] != "cpu" {
		return 0
	}
	var sum uint64
	for _, f := range fields[1:] {
		n, _ := strconv.ParseUint(f, 10, 64)
		sum += n
	}
	return sum
}
