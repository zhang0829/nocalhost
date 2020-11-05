/*
Copyright 2020 The Nocalhost Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package routers

import (
	"nocalhost/pkg/nocalhost-api/app/api/v1/application_cluster"
	"nocalhost/pkg/nocalhost-api/app/api/v1/applications"
	"nocalhost/pkg/nocalhost-api/app/api/v1/cluster"
	"nocalhost/pkg/nocalhost-api/app/api/v1/cluster_user"
	"nocalhost/pkg/nocalhost-api/napp"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"

	ginSwagger "github.com/swaggo/gin-swagger" //nolint: goimports
	"github.com/swaggo/gin-swagger/swaggerFiles"

	"nocalhost/pkg/nocalhost-api/app/api"
	"nocalhost/pkg/nocalhost-api/app/api/v1/user"

	// import swagger handler
	_ "nocalhost/docs" // docs is generated by Swag CLI, you have to import it.
	"nocalhost/pkg/nocalhost-api/app/router/middleware"
)

// Load loads the middlewares, routes, handlers.
func Load(g *gin.Engine, mw ...gin.HandlerFunc) *gin.Engine {
	// 使用中间件
	g.Use(middleware.NoCache)
	g.Use(middleware.Options)
	g.Use(middleware.Secure)
	g.Use(middleware.Logging())
	g.Use(middleware.RequestID())
	g.Use(mw...)

	// 404 Handler.
	g.NoRoute(api.RouteNotFound)
	g.NoMethod(api.RouteNotFound)

	// 静态资源
	//g.Static("/static", "./static")

	// 仅在test环境下开启，线上关闭
	if viper.GetString("app.run_mode") == napp.ModeDebug {
		// swagger api docs
		g.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		// pprof router 性能分析路由
		// 默认关闭，开发环境下可以打开
		// 访问方式: HOST/debug/pprof
		// 通过 HOST/debug/pprof/profile 生成profile
		// 查看分析图 go tool pprof -http=:5000 profile
		// see: https://github.com/gin-contrib/pprof
		pprof.Register(g)
	} else {
		// disable swagger docs for release  env=release
		g.GET("/swagger/*any", ginSwagger.DisablingWrapHandler(swaggerFiles.Handler, "env"))
	}

	g.POST("/v1/register", user.Register)
	g.POST("/v1/login", user.Login)

	u := g.Group("/v1/users")
	u.Use(middleware.AuthMiddleware())
	{
		u.GET("", user.Get)
		u.GET("/list", user.GetList)
		u.POST("", user.Create)
		u.PUT("/:id", user.Update)
		u.DELETE("/:id", user.Delete)
	}

	// 集群
	c := g.Group("/v1/cluster")
	c.Use(middleware.AuthMiddleware())
	{
		c.POST("", cluster.Create)
		c.GET("/list", cluster.GetList)
	}

	// 应用
	a := g.Group("/v1/application")
	a.Use(middleware.AuthMiddleware())
	{
		a.POST("", applications.Create)
		a.GET("", applications.Get)
		a.DELETE("/:id", applications.Delete)
		a.PUT("/:id", applications.Update)
		a.POST("/:id/bind_cluster", application_cluster.Create)
		a.POST("/:id/create_space", cluster_user.Create)
	}

	return g
}
