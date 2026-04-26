package model

type UserInfo struct {
	Name           string          `json:"name"`
	Email          string          `json:"email"`
	Phone          string          `json:"phone"`
	Location       string          `json:"location"`
	LinkedIn       string          `json:"linkedin"`
	GitHub         string          `json:"github"`
	Education      []Education     `json:"education"`
	Experience     []Experience    `json:"experience"`
	Projects       []Project       `json:"projects"`
	Skills         Skills          `json:"skills"`
	Awards         []string        `json:"awards"`
}

type Education struct {
	Degree      string   `json:"degree"`
	Institution string   `json:"institution"`
	Location    string   `json:"location"`
	StartDate   string   `json:"start_date"`
	EndDate     string   `json:"end_date"`
	Coursework  []string `json:"coursework"`
}

type Experience struct {
	Title     string   `json:"title"`
	Company   string   `json:"company"`
	Location  string   `json:"location"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
	Bullets   []string `json:"bullets"`
	Keywords  []string `json:"keywords"`
}

type Project struct {
	Name         string   `json:"name"`
	Technologies []string `json:"technologies"`
	Bullets      []string `json:"bullets"`
}

type Skills struct {
	Languages  []string `json:"languages"`
	Frameworks []string `json:"frameworks"`
	DevTools   []string `json:"dev_tools"`
	Databases  []string `json:"databases"`
}
