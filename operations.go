package main

import (
	"fmt"
	"jiraToClubhouseCLI/internal/clubHouse"
	"jiraToClubhouseCLI/internal/jira"
	"strconv"
	"strings"
	"time"

	"github.com/kennygrant/sanitize"
)

type jiraItem jira.Item
type jiraExport jira.Export
type jiraAttachment jira.Attachment
type jiraComment jira.Comment

func GetUserInfo(userMaps []userMap, jiraUsername string) (CHID string) {

	defaultUser := ""

	for _, u := range userMaps {
		if u.JiraUsername == jiraUsername {
			return u.CHID
		}
		if u.Default == true {
			defaultUser = u.CHID
		}
	}
	if defaultUser != "" {
		fmt.Printf("Unknown user %s. Will use default user: %s\n\n", jiraUsername, defaultUser)
	} else {
		fmt.Printf("Unknown user %s. No default user defined in userMap. This story will not be created\n\n", jiraUsername)
	}

	return defaultUser
}

func GetProjectInfo(projectMaps []projectMap, jiraProjectKey string) (CHProjectID int) {

	for _, u := range projectMaps {
		// fmt.Printf("JiraProjectKey: %s | CHProjectID: %d\n\n", u.JiraProjectKey, u.CHProjectID)
		if u.JiraProjectKey == jiraProjectKey {
			return u.CHProjectID
		}
	}
	return 0
}

//GetDataForClubhouse will take the data from the XML and translate it into a format for sending to Clubhouse
func (je *jiraExport) GetDataForClubhouse(userMaps []userMap, projectMaps []projectMap) clubHouse.Data {
	epics := []jira.Item{}
	tasks := []jira.Item{}
	stories := []jira.Item{}

	for _, item := range je.Items {
		switch item.Type {
		case "Epic":
			epics = append(epics, item)
			break
		case "Sub-task":
			tasks = append(tasks, item)
			break
		default:
			stories = append(stories, item)
			break
		}
	}

	chEpics := []clubHouse.CreateEpic{}

	for _, item := range epics {
		chEpics = append(chEpics, item.CreateEpic())
	}

	chTasks := []clubHouse.CreateTask{}
	chStories := []clubHouse.CreateStory{}

	for _, item := range tasks {
		chTasks = append(chTasks, item.CreateTask())
	}

	for _, item := range stories {
		chStories = append(chStories, item.CreateStory(userMaps, projectMaps))
	}

	// storyMap is used to link the JiraItem's key to its index in the chStories slice. This is then used to assign subtasks properly
	storyMap := make(map[string]int)
	for i, item := range chStories {
		storyMap[item.ExternalID] = i
	}

	for _, task := range chTasks {
		chStories[storyMap[task.Parent]].Tasks = append(chStories[storyMap[task.Parent]].Tasks, task)
	}

	return clubHouse.Data{Epics: chEpics, Stories: chStories}
}

// CreateEpic returns a CreateEpic from the JiraItem
func (item *jiraItem) CreateEpic() clubHouse.CreateEpic {
	fmt.Printf("Epic Name: %s | Description: %s | Summary: %s\n\n", item.GetEpicName(), item.Description, item.Summary)

	return clubHouse.CreateEpic{Description: sanitize.HTML(item.Summary + "<br><br>" + item.Description), Name: sanitize.HTML(item.GetEpicName()), ExternalID: item.Key, CreatedAt: ParseJiraTimeStamp(item.CreatedAtString)}
}

// CreateTask returns a task if the item is a Jira Sub-task
func (item *jiraItem) CreateTask() clubHouse.CreateTask {
	return clubHouse.CreateTask{Description: sanitize.HTML(item.Summary), Parent: item.Parent, Complete: false}
}

// CreateStory re from the JiraItem
func (item *jiraItem) CreateStory(userMaps []userMap, projectMaps []projectMap) clubHouse.CreateStory {
	// fmt.Println("assignee: ", item.Assignee, "reporter: ", item.Reporter)
	//{}

	attachments := []clubHouse.CreateAttachment{}
	for _, attch := range item.Attachments {
		attachments = append(attachments, attch.CreateAttachment(userMaps))
	}

	comments := []clubHouse.CreateComment{}
	for _, c := range item.Comments {
		comments = append(comments, c.CreateComment(userMaps))
	}

	labels := []clubHouse.CreateLabel{}
	for _, label := range item.Labels {
		labels = append(labels, clubHouse.CreateLabel{Name: strings.ToLower(label)})
	}
	// Adding special label that indicates that it was imported from JIRA
	labels = append(labels, clubHouse.CreateLabel{Name: "JIRA"})

	// Adding Sprint as label
	sprintLabel := item.GetSprint()
	if sprintLabel != "" {
		labels = append(labels, clubHouse.CreateLabel{Name: sprintLabel})
	}

	// Overwrite supplied Project ID
	projectID := MapProject(projectMaps, item.Project.Key)
	// projectID, ownerID := GetUserInfo(userMaps, item.Assignee.Username)

	// Map JIRA assignee to Clubhouse owner(s)
	// Leave array empty if username is unknown
	// Must use "make" function to force empty array for correct JSON marshalling
	ownerID := MapUser(userMaps, item.Assignee.Username)
	var owners []string
	if ownerID != "" {
		// owners := []string{ownerID}
		owners = append(owners, ownerID)
	} else {
		owners = make([]string, 0)
	}

	// Map JIRA status to Clubhouse Workflow state
	// cases break automatically, no fallthrough by default
	var state int64 = 500000014
	switch item.Status {
	case "Open":
		// Open
		state = 500000003
	case "Done":
		// Done
		state = 500000002
	case "In Development":
		// In Development
		state = 500000004
	case "Waiting for Code Review":
		// Ready for Review
		state = 500000005
	case "In Code Review":
		// In Review
		state = 500000018
	case "Waiting for UX-Interaction Design Review":
		// Ready for Review
		state = 500000005
	case "In UX-Interaction Design Review":
		// In Review
		state = 500000018
	case "Waiting for UX-Design Review":
		// Ready for Review
		state = 500000005
	case "In UX-Design Review":
		// In Review
		state = 500000018
	case "Waiting for QA":
		// Ready for Test
		state = 500000017
	case "In QA":
		// In Test
		state = 500000019
	case "In QA Review":
		// In Test
		state = 500000019
	default:
		// Open
		state = 500000003
	}

	requestor := MapUser(userMaps, item.Reporter.Username)
	// _, requestor := GetUserInfo(userMaps, item.Reporter.Username)

	fmt.Printf("%s: JIRA Assignee: %s | Project: %d | Status: %s | Description: %s | Estimate: %d | Epic Link: %s | SprintTag: %s\n\n", item.Key, item.Assignee.Username, projectID, item.Status, item.GetDescription(), item.GetEstimate(), item.GetEpicLink(), item.GetSprint())

	return clubHouse.CreateStory{
		Comments:      comments,
		CreatedAt:     ParseJiraTimeStamp(item.CreatedAtString),
		Description:   item.GetDescription(),
		ExternalID:    item.Key,
		Labels:        labels,
		Name:          sanitize.HTML(item.Summary),
		ProjectID:     int64(projectID),
		StoryType:     item.GetClubhouseType(),
		EpicLink:      item.GetEpicLink(),
		WorkflowState: state,
		OwnerIDs:      owners,
		RequestedBy:   requestor,
		Estimate:      item.GetEstimate(),
	}
}

func (attachment *jiraAttachment) CreateAttachment(userMaps []userMap) clubHouse.CreateAttachment {
	author := MapUser(userMaps, attachment.Author)

	return clubHouse.CreateAttachment{
		Author:     author,
		CreatedAt:  ParseJiraTimeStamp(attachment.CreatedAtString),
		ExternalID: attachment.ID,
		Name:       attachment.Name,
	}
}

// CreateComment takes the JiraItem's comment data and returns a CreateComment
func (comment *jiraComment) CreateComment(userMaps []userMap) clubHouse.CreateComment {
	commentText := sanitize.HTML(comment.Comment)
	if commentText == "\n" {
		commentText = "(empty)"
	}
	author := MapUser(userMaps, comment.Author)

	return clubHouse.CreateComment{
		Text:      commentText,
		CreatedAt: ParseJiraTimeStamp(comment.CreatedAtString),
		Author:    author,
	}
}

// GetEpicLink returns the Epic Link of a Jira Item.
func (item *jiraItem) GetEpicLink() string {
	for _, cf := range item.CustomFields {
		if cf.FieldName == "Epic Link" {
			return cf.FieldVales[0]
		}
	}
	return ""
}

// GetAcceptanceCriteria returns the acceptance criteria
func (item *jiraItem) GetAcceptanceCriteria() string {
	for _, cf := range item.CustomFields {
		if cf.FieldName == "Acceptance Criteria" {
			header := "<br>## Acceptance Criteria<br>"
			return header + cf.FieldVales[0]
		}
	}
	return ""
}

// GetEstimate returns the Story Points
func (item *jiraItem) GetEstimate() int64 {
	for _, cf := range item.CustomFields {
		if cf.FieldName == "Story Points" {
			storyPoint := cf.FieldVales[0]
			return ParseFloatStringToInt(storyPoint)
		}
	}
	return 0
}

// ParseFloatStringToInt parses a string containing a float into an uprounded int
func ParseFloatStringToInt(sFloat string) int64 {
	f, err := strconv.ParseFloat(sFloat, 64)
	if err == nil {
		i := int64(f + 0.5)
		return i
	}
	return 0
}

// GetDescription returns a concatenation of description and acceptance criteria
func (item *jiraItem) GetDescription() string {
	return sanitize.HTML(item.Description + item.GetAcceptanceCriteria())
}

// GetSprint returns a string to be used as tag for srint grouping
func (item *jiraItem) GetSprint() string {

	for _, cf := range item.CustomFields {
		if cf.FieldName == "Sprint" {
			sprint := cf.FieldVales[0]

			startPoint := strings.Index(sprint, "Sprint")
			if startPoint == -1 {
				startPoint = 0
			}
			sprintAfterNoise := sprint[startPoint:len(sprint)] + " " + item.Project.Key
			sprintAsTag := strings.ToLower(strings.Replace(sprintAfterNoise, " ", "_", -1))

			return sprintAsTag
		}
	}
	return ""

}

// GetEpicName returns the name of an epic stored in custom fields
func (item *jiraItem) GetEpicName() string {
	for _, cf := range item.CustomFields {
		if cf.FieldName == "Epic Name" {
			epicName := cf.FieldVales[0]
			return epicName
		}
	}
	return ""
}

// GetClubhouseType determines type based on if the Jira item is a bug or not.
func (item *jiraItem) GetClubhouseType() string {
	if item.Type == "Bug" {
		return "bug"
	}
	return "feature"
}

// ParseJiraTimeStamp parses the format in the XML using Go's magical timestamp.
func ParseJiraTimeStamp(dateString string) time.Time {
	format := "Mon, 2 Jan 2006 15:04:05 -0700"
	t, err := time.Parse(format, dateString)
	if err != nil {
		return time.Now()
	}
	return t
}