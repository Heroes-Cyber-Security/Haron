package types

type CTFDUser struct {
	Id   int
	Name string
}

func (user CTFDUser) IsValid() bool {
	return user.Id > 0 && user.Name != "invalid"
}

func (user CTFDUser) ToJSON() map[string]any {
	return map[string]any{
		"id":   user.Id,
		"name": user.Name,
	}
}
