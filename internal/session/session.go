package session

import "time"

// Session is the provider-neutral representation stored in the local index.
type Session struct {
	Provider  string    `json:"provider"`
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Directory string    `json:"directory"`
	UpdatedAt time.Time `json:"updatedAt"`
	Content   string    `json:"-"`
	Source    string    `json:"-"`
	Stamp     int64     `json:"-"`
}

func (s Session) Key() string {
	return s.Provider + ":" + s.ID
}

// Match is a search result with an optional matching content excerpt.
type Match struct {
	Session
	Snippet string  `json:"snippet,omitempty"`
	Score   float64 `json:"score,omitempty"`
}
