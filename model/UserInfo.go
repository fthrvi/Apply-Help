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
	// Transcript is an optional, free-form list of courses (one per
	// entry). The LLM picks the 2-4 most relevant to the target role
	// when generating EducationCoursework[i]; entries without a
	// transcript fall back to the Coursework list.
	Transcript []string `json:"transcript,omitempty"`
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
	URL          string   `json:"url,omitempty"`
	Technologies []string `json:"technologies"`
	Bullets      []string `json:"bullets"`
}

type Skills struct {
	Languages  []string `json:"languages"`
	Frameworks []string `json:"frameworks"`
	DevTools   []string `json:"dev_tools"`
	Databases  []string `json:"databases"`
}
