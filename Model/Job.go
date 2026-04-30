package model

import (
	"strconv"
)

type Job struct {
	Id          int
	Company     string
	Role        string
	Link        string
	Status      string
	Created     string
	Description string
	Resume      string
	Coverletter string
	Question    string
	ResumeData  string // JSON string
	CoverData   string // JSON string
	Source      string
	HasDocument int
}

func (j Job) ToStringSlice() []string {
	return []string{
		strconv.Itoa(j.Id),
		j.Company,
		j.Role,
		j.Link,
		j.Status,
	}
}
