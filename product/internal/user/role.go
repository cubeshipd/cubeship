package user

import "time"

// AccessRole is a named set of grants, given to members and to API keys.
//
// **Named, and given, rather than composed on each account.** Access is
// usually the same for several people or several agents, and a role
// changed once changes all of them — which is also what makes "who can
// touch the databases" answerable from one screen.
type AccessRole struct {
	ID          int64
	Name        string
	Description string
	Grants      []Grant
	// Members and Keys are how many accounts and keys hold it.
	Members   int
	Keys      int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Policy is the role's grants as a policy.
func (r *AccessRole) Policy() Policy { return NewPolicy(r.Grants) }
