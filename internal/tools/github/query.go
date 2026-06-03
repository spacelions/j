package github

// prFirstPageQuery fetches the PR header plus the first page of
// conversation comments, review threads (with their first page of
// inline comments), and reviews. Follow-up pages for each connection
// land via the dedicated `*PageQuery` strings below so the common
// "everything fits in one page" case is a single round-trip.
const prFirstPageQuery = `query($o:String!,$r:String!,$n:Int!){
  repository(owner:$o,name:$r){
    pullRequest(number:$n){
      url state isDraft merged
      comments(first:100){
        pageInfo{endCursor hasNextPage}
        nodes{id author{login} body}
      }
      reviewThreads(first:100){
        pageInfo{endCursor hasNextPage}
        nodes{
          id isOutdated
          comments(first:100){
            pageInfo{endCursor hasNextPage}
            nodes{id author{login} body path line}
          }
        }
      }
      reviews(first:100){
        pageInfo{endCursor hasNextPage}
        nodes{id author{login} body state}
      }
    }
  }
}`

// prCommentsPageQuery paginates additional pages of the PR's
// top-level conversation comments.
const prCommentsPageQuery = `query($o:String!,$r:String!,$n:Int!,$a:String!){
  repository(owner:$o,name:$r){
    pullRequest(number:$n){
      comments(first:100,after:$a){
        pageInfo{endCursor hasNextPage}
        nodes{id author{login} body}
      }
    }
  }
}`

// prReviewThreadsPageQuery paginates additional pages of review
// threads. Inner thread comments stay capped at 100 in v1 — the
// rare >100-comment thread surfaces only its first page; the
// follow-up pages are not fetched.
const prReviewThreadsPageQuery = `query(
  $o:String!,$r:String!,$n:Int!,$a:String!
){
  repository(owner:$o,name:$r){
    pullRequest(number:$n){
      reviewThreads(first:100,after:$a){
        pageInfo{endCursor hasNextPage}
        nodes{
          id isOutdated
          comments(first:100){
            pageInfo{endCursor hasNextPage}
            nodes{id author{login} body path line}
          }
        }
      }
    }
  }
}`

// prReviewsPageQuery paginates additional pages of pull-request
// reviews (the bodied summary objects, distinct from review
// threads).
const prReviewsPageQuery = `query($o:String!,$r:String!,$n:Int!,$a:String!){
  repository(owner:$o,name:$r){
    pullRequest(number:$n){
      reviews(first:100,after:$a){
        pageInfo{endCursor hasNextPage}
        nodes{id author{login} body state}
      }
    }
  }
}`

type prAuthor struct {
	Login string `json:"login"`
}

type prPageInfo struct {
	EndCursor   string `json:"endCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

type prConversationComment struct {
	ID     string   `json:"id"`
	Author prAuthor `json:"author"`
	Body   string   `json:"body"`
}

type prConversationCommentsPage struct {
	PageInfo prPageInfo              `json:"pageInfo"`
	Nodes    []prConversationComment `json:"nodes"`
}

type prReviewComment struct {
	ID     string   `json:"id"`
	Author prAuthor `json:"author"`
	Body   string   `json:"body"`
	Path   string   `json:"path"`
	Line   int      `json:"line"`
}

type prReviewCommentsPage struct {
	PageInfo prPageInfo        `json:"pageInfo"`
	Nodes    []prReviewComment `json:"nodes"`
}

type prReviewThread struct {
	ID         string               `json:"id"`
	IsOutdated bool                 `json:"isOutdated"`
	Comments   prReviewCommentsPage `json:"comments"`
}

type prReviewThreadsPage struct {
	PageInfo prPageInfo       `json:"pageInfo"`
	Nodes    []prReviewThread `json:"nodes"`
}

type prReview struct {
	ID     string   `json:"id"`
	Author prAuthor `json:"author"`
	Body   string   `json:"body"`
	State  string   `json:"state"`
}

type prReviewsPage struct {
	PageInfo prPageInfo `json:"pageInfo"`
	Nodes    []prReview `json:"nodes"`
}

type prFirstPage struct {
	URL           string                     `json:"url"`
	State         string                     `json:"state"`
	IsDraft       bool                       `json:"isDraft"`
	Merged        bool                       `json:"merged"`
	Comments      prConversationCommentsPage `json:"comments"`
	ReviewThreads prReviewThreadsPage        `json:"reviewThreads"`
	Reviews       prReviewsPage              `json:"reviews"`
}

type prFirstPageResponse struct {
	Data struct {
		Repository *struct {
			PullRequest *prFirstPage `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type prCommentsPageResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				Comments prConversationCommentsPage `json:"comments"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type prReviewThreadsPageResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads prReviewThreadsPage `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type prReviewsPageResponse struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				Reviews prReviewsPage `json:"reviews"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}
