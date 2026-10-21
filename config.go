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
	valid := c.Items[:0]
	for _, it := range c.Items {
		if it.ID == "" {
			it.ID = newID()
		}
		if it.Seconds < 1 {
			it.Seconds = 1
		}
		if !it.Source.valid() {
			it.Source = srcStatic
		}
		valid = append(valid, it)
	}
	c.Items = valid
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

// best-effort; persistence failures never interrupt the session
func writeJSON(path string, v any) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0o644)
}
