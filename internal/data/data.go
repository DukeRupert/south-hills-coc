package data

import (
	"embed"
	"log"

	"gopkg.in/yaml.v3"
)

//go:embed leadership.yaml ministries.yaml
var dataFS embed.FS

type StaffMember struct {
	Name  string `yaml:"name"`
	Role  string `yaml:"role"`
	Email string `yaml:"email"`
	Image string `yaml:"image"`
	Bio   string `yaml:"bio"`
}

type Elder struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

type Deacon struct {
	Name       string `yaml:"name"`
	Department string `yaml:"department"`
	Image      string `yaml:"image"`
}

type LeadershipData struct {
	Staff   []StaffMember `yaml:"staff"`
	Elders  []Elder       `yaml:"elders"`
	Deacons []Deacon      `yaml:"deacons"`
}

type Ministry struct {
	Name        string `yaml:"name"`
	Slug        string `yaml:"slug"`
	Description string `yaml:"description"`
	Contact     string `yaml:"contact"`
}

type MinistriesData struct {
	Growth         []Ministry `yaml:"growth"`
	NextGeneration []Ministry `yaml:"nextGeneration"`
	Service        []Ministry `yaml:"service"`
}

func LoadLeadership() *LeadershipData {
	b, err := dataFS.ReadFile("leadership.yaml")
	if err != nil {
		log.Fatalf("failed to read leadership.yaml: %v", err)
	}
	var d LeadershipData
	if err := yaml.Unmarshal(b, &d); err != nil {
		log.Fatalf("failed to parse leadership.yaml: %v", err)
	}
	return &d
}

func LoadMinistries() *MinistriesData {
	b, err := dataFS.ReadFile("ministries.yaml")
	if err != nil {
		log.Fatalf("failed to read ministries.yaml: %v", err)
	}
	var d MinistriesData
	if err := yaml.Unmarshal(b, &d); err != nil {
		log.Fatalf("failed to parse ministries.yaml: %v", err)
	}
	return &d
}
