package core

import (
	"encoding/json"
	"os"
)

const settingsFile = "runtime.json"

type settingsService struct {
	loadpoints map[interface{}]int
	data       settingsData
}

type settingsData struct {
	Loadpoints map[int]map[string]interface{} `json:"loadpoints,omitempty"`
}

var settings *settingsService

func init() {
	settings = &settingsService{
		loadpoints: make(map[interface{}]int),
		data: settingsData{
			Loadpoints: map[int]map[string]interface{}{},
		},
	}

	settings.load()
}

func (s *settingsService) Add(id int, lp interface{}) {
	s.loadpoints[lp] = id
}

func (s *settingsService) lp(lp interface{}) (int, map[string]interface{}) {
	id := s.loadpoints[lp] + 1

	if _, ok := s.data.Loadpoints[id]; !ok {
		s.data.Loadpoints[id] = make(map[string]interface{})
	}

	return id, s.data.Loadpoints[id]
}

func (s *settingsService) Load(lp interface{}, data interface{}) {
	b, err := os.ReadFile(settingsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		panic(err)
	}

	err = json.Unmarshal(b, &s.data)
	if err != nil {
		panic(err)
	}
}

func (s *settingsService) Save(lp interface{}, data interface{}) {
	b, err := json.Marshal(s.data)
	if err != nil {
		panic(err)
	}

	err = os.WriteFile(settingsFile, b, 0644)
	if err != nil {
		panic(err)
	}
}

func (s *settingsService) Set(lp interface{}, key string, val interface{}) {
	_, data := s.lp(lp)
	data[key] = val
	s.save()
}

func (s *settingsService) Get(lp interface{}, key string) interface{} {
	_, data := s.lp(lp)
	return data[key]
}
