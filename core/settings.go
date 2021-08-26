package core

import (
	"encoding/json"
	"fmt"
	"os"
)

const settingsFile = "runtime.json"

type settingsService struct {
	loadpoints map[interface{}]int
	data       map[string]RuntimeSettings
}

var settings *settingsService

func init() {
	settings = &settingsService{
		loadpoints: make(map[interface{}]int),
		data:       make(map[string]RuntimeSettings),
	}

	// settings.load()
}

func (s *settingsService) Add(id int, lp interface{}) {
	s.loadpoints[lp] = id
}

func (s *settingsService) lp(lp interface{}) string {
	id := s.loadpoints[lp] + 1
	return fmt.Sprintf("lp%d", id)
}

func (s *settingsService) Load(lp interface{}) (RuntimeSettings, error) {
	b, err := os.ReadFile(settingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return RuntimeSettings{}, nil
		}
		return RuntimeSettings{}, err
	}

	err = json.Unmarshal(b, &s.data)
	if err != nil {
		return RuntimeSettings{}, err
	}

	id := s.lp(lp)

	return s.data[id], nil
}

func (s *settingsService) Save(lp interface{}, node RuntimeSettings) error {
	id := s.lp(lp)
	s.data[id] = node

	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(settingsFile, b, 0644)
}

// func (s *settingsService) Set(lp interface{}, key string, val interface{}) {
// 	_, data := s.lp(lp)
// 	data[key] = val
// 	s.save()
// }

// func (s *settingsService) Get(lp interface{}, key string) interface{} {
// 	_, data := s.lp(lp)
// 	return data[key]
// }
