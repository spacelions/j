package linear

// WorkflowState is the shape returned by ListTeamWorkflowStates: the
// id is the GraphQL node id (used by issueUpdate to move an issue),
// Name is the human-readable label ("Todo", "In Progress", "In
// Review"), and Type is Linear's enum bucket
// (backlog/unstarted/started/completed/canceled).
type WorkflowState struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

const teamWorkflowStatesQuery = `query($id:String!){` +
	`issue(id:$id){team{states{nodes{id name type}}}}}`

const viewerIDQuery = `query{viewer{id}}`

type teamWorkflowStatesResponse struct {
	Data struct {
		Issue *struct {
			Team struct {
				States struct {
					Nodes []WorkflowState `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type viewerIDResponse struct {
	Data struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// FindStateByName returns the workflow state in states whose Name
// matches name, or (zero, false) when no match exists. Used by the
// state-sync hook in internal/lifecycle to resolve a label like
// "In Progress" to the node id Linear's issueUpdate mutation
// expects.
func FindStateByName(
	states []WorkflowState, name string,
) (WorkflowState, bool) {
	for _, s := range states {
		if s.Name == name {
			return s, true
		}
	}
	return WorkflowState{}, false
}
