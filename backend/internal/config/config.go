package config

import "github.com/zeromicro/go-zero/rest"

type Config struct {
	rest.RestConf
	DataSource string `json:"datasource"`
	RedisAddr  string `json:"redis_addr"`
	JwtSecret  string `json:"jwt_secret"`
}
