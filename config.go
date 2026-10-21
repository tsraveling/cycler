package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

// @region app:config -- CYCLER ITEMS + PREFS

const (
	maxNameLen = 24
	maxMinutes = 180
)

type item struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Seconds      int        `json:"seconds"`
	Source       sourceType `json:"source"`
	SourceConfig string     `json:"sourceConfig"`
}

type config struct {
	Items    []item `json:"items"`
	HelpFull bool   `json:"helpFull"`
}

// overridable so tests never touch the real home directory
var configDirFn = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	return filepath.Join(home, ".config", "cycler")
}

func configPath() string { return filepath.Join(configDirFn(), "config.json") }

// a missing or corrupt file yields an empty config rather than an error
func loadConfig() *config {
	c := &config{}
	data, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	if json.Unmarshal(data, c) != nil {
		return &config{}
	}
	seen := map[string]bool{}
	repaired := false
	valid := c.Items[:0]
	for _, it := range c.Items {
		if it.ID == "" || seen[it.ID] {
			it.ID = newID()
			repaired = true
		}
		seen[it.ID] = true
		if it.Seconds < 1 {
			it.Seconds = 1
		}
		if !it.Source.valid() {
			it.Source = srcStatic
		}
		valid = append(valid, it)
	}
	c.Items = valid
	if repaired {
		c.save()
	}
	return c
}

func (c *config) save() {
	writeJSON(configPath(), c)
}

func (c *config) indexOf(id string) int {
	for i, it := range c.Items {
		if it.ID == id {
			return i
		}
	}
	return -1
}

func (c *config) byID(id string) *item {
	if i := c.indexOf(id); i >= 0 {
		return &c.Items[i]
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// best-effort atomic write: temp file, then rename over the target. a
// symlinked target is written through to the file it points at.
func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	dir := filepath.Dir(path)
	if os.MkdirAll(dir, 0o755) != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return
	}
	_, werr := tmp.Write(data)
	serr := tmp.Sync()
	cerr := tmp.Close()
	os.Chmod(tmp.Name(), mode)
	if werr != nil || serr != nil || cerr != nil || os.Rename(tmp.Name(), path) != nil {
		os.Remove(tmp.Name())
	}
}
