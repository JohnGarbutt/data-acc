package fakewarp

type pool struct {
	Id          string `json:"id"`
	Units       string `json:"units"`
	Granularity uint   `json:"granularity"`
	Quantity    uint   `json:"quantity"`
	Free        uint   `json:"free"`
}

type pools []pool

func (list *pools) String() string {
	message := map[string]pools{"pools": *list}
	return toJson(message)
}

func GetPools() *pools {
	fakePool := pool{"fake", "bytes", 214748364800, 40, 3}
	return &pools{fakePool}
}