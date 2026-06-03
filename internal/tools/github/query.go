package github

const prQuery = `query($owner:String!,$repo:String!,$number:Int!){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      url state isDraft merged
      comments(first:100){
        nodes{databaseId author{login} body}
      }
      reviewThreads(first:100){
        nodes{
          id isOutdated
          comments(first:100){
            nodes{
              databaseId author{login} body path line
            }
          }
        }
      }
    }
  }
}`

type prAuthor struct {
	Login string `json:"login"`
}

type prConversationComment struct {
	DatabaseID int64    `json:"databaseId"`
	Author     prAuthor `json:"author"`
	Body       string   `json:"body"`
}

type prReviewComment struct {
	DatabaseID int64    `json:"databaseId"`
	Author     prAuthor `json:"author"`
	Body       string   `json:"body"`
	Path       string   `json:"path"`
	Line       int      `json:"line"`
}

type prReviewThread struct {
	ID         string `json:"id"`
	IsOutdated bool   `json:"isOutdated"`
	Comments   struct {
		Nodes []prReviewComment `json:"nodes"`
	} `json:"comments"`
}

type prResponse struct {
	Data struct {
		Repository *struct {
			PullRequest *struct {
				URL      string `json:"url"`
				State    string `json:"state"`
				IsDraft  bool   `json:"isDraft"`
				Merged   bool   `json:"merged"`
				Comments struct {
					Nodes []prConversationComment `json:"nodes"`
				} `json:"comments"`
				ReviewThreads struct {
					Nodes []prReviewThread `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}
