package auth

const (
	InfiniteCodeAccessTokenPrefix  = "ic_at_"
	InfiniteCodeRefreshTokenPrefix = "ic_rt_"
)

type InfiniteCodeTokenPrincipal struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
}

func GenerateInfiniteCodeAccessToken() (string, error) {
	token, err := randomString(32)
	if err != nil {
		return "", err
	}
	return InfiniteCodeAccessTokenPrefix + token, nil
}

func GenerateInfiniteCodeRefreshToken() (string, error) {
	token, err := randomString(36)
	if err != nil {
		return "", err
	}
	return InfiniteCodeRefreshTokenPrefix + token, nil
}

func InfiniteCodeTokenRedisKey(kind string, token string) string {
	return "infinite-code:" + kind + ":" + HashAPIKey(token)
}
