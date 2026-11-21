package types

// CTFUser is the platform-independent player identity Haron authenticates
// against. CTFd exposes numeric IDs while rCTF uses opaque string IDs, so the
// field is kept as a string for both.
type CTFUser struct {
	Id   string
	Name string
}

func (user CTFUser) IsValid() bool {
	return user.Name != "" && user.Name != "invalid"
}

func (user CTFUser) ToJSON() map[string]any {
	if !user.IsValid() {
		return nil
	}
	return map[string]any{
		"id":   user.Id,
		"name": user.Name,
	}
}
