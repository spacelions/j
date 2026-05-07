package linear

// Issue is the shape returned by GetIssue and ListAssignedIssues.
// ID is the GraphQL node id (opaque) — required by the issueUpdate
// and commentCreate mutations, which address issues by node id, not
// by `<TEAM>-<NUM>` identifier. Identifier is the upstream `ENG-123`
// form; URL is the public web link the markdown footer echoes back.
// State is the human-readable workflow state name (e.g. "In
// Progress"), populated by the list-issues path; GetIssue does not
// fetch it and leaves it empty. Description is set by GetIssue and
// empty on list responses (the list view doesn't fetch bodies).
type Issue struct {
	ID          string `json:"id"`
	Identifier  string `json:"identifier"`
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	State       string `json:"-"`
}

// Project is the shape returned by ListProjects. ID is the GraphQL
// node id (opaque); Name is the human-readable label rendered by the
// picker.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListIssuesOpts narrows ListAssignedIssues. ProjectID, when set,
// limits the result to issues whose project.id equals it; empty
// means "any project (or none)".
type ListIssuesOpts struct {
	ProjectID string
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

// firstGraphQLError returns the message of the first error in errs,
// or "" when the slice is empty. Linear surfaces multiple
// validation errors per response; the client paths only need the
// first one to stage a wrapped `linear: <msg>` error to the caller.
func firstGraphQLError(errs []graphQLError) string {
	if len(errs) == 0 {
		return ""
	}
	return errs[0].Message
}

const issueQuery = `query($id:String!){` +
	`issue(id:$id){id identifier title description url}}`

const projectsQuery = `query{projects{nodes{id name}}}`

const assignedIssuesQuery = `query{viewer{assignedIssues(` +
	`filter:{state:{type:{eq:"backlog"}}},` +
	`orderBy:updatedAt,first:50){` +
	`nodes{identifier title url state{name}}}}}`

const assignedIssuesByProjectQuery = `query($projectId:ID!){` +
	`viewer{assignedIssues(filter:{` +
	`state:{type:{eq:"backlog"}},` +
	`project:{id:{eq:$projectId}}},` +
	`orderBy:updatedAt,first:50){` +
	`nodes{identifier title url state{name}}}}}`

type issueResponse struct {
	Data struct {
		Issue *Issue `json:"issue"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type projectsResponse struct {
	Data struct {
		Projects struct {
			Nodes []Project `json:"nodes"`
		} `json:"projects"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// assignedIssueNode mirrors the `nodes` shape inside
// viewer.assignedIssues. State arrives nested as `state.name` so the
// node has its own struct; ListAssignedIssues flattens each node
// into an Issue before returning.
type assignedIssueNode struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      struct {
		Name string `json:"name"`
	} `json:"state"`
}

type assignedIssuesResponse struct {
	Data struct {
		Viewer struct {
			AssignedIssues struct {
				Nodes []assignedIssueNode `json:"nodes"`
			} `json:"assignedIssues"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}
