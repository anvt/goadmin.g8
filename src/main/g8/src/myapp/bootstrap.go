/*
Package myapp contains application's source code.
*/
package myapp

import (
	"github.com/btnguyen2k/consu/reddo"
	"github.com/btnguyen2k/prom"
	"github.com/go-akka/configuration"
	"github.com/labstack/echo/v4"
	"html/template"
	"io"
	"log"
	"main/src/goadmin"
	"main/src/i18n"
	"net/http"
	"reflect"
	"strings"
)

type MyBootstrapper struct {
	name string
}

var (
	Bootstrapper = &MyBootstrapper{name: "myapp"}
	cdnMode      = false
	myStaticPath = "/static"
	myI18n       *i18n.I18n
	sqlc         *prom.SqlConnect
	groupDao     GroupDao
	userDao      UserDao
)

const (
	namespace = "myapp"

	sessionMyUid = "uid"

	actionNameHome          = "home"
	actionNameCpLogin       = "cp_login"
	actionNameCpLoginSubmit = "cp_login_submit"
	actionNameCpLogout      = "cp_logout"
	actionNameCpDashboard   = "cp_dashboard"
)

/*
Bootstrap implements goadmin.IBootstrapper.Bootstrap

Bootstrapper usually does:
- register URI routing
- other initializing work (e.g. creating DAO, initializing database, etc)
*/
func (b *MyBootstrapper) Bootstrap(conf *configuration.Config, e *echo.Echo) error {
	cdnMode = conf.GetBoolean(goadmin.ConfKeyCdnMode, false)

	myStaticPath = "/static_v" + conf.GetString("app.version", "")
	e.Static(myStaticPath, "public")

	myI18n = i18n.NewI18n("./config/i18n_" + namespace)

	initDaos()

	// register a custom namespace-scope template renderer
	goadmin.EchoRegisterRenderer(namespace, newTemplateRenderer("./views/myapp", ".html"))

	e.GET("/", actionHome).Name = actionNameHome

	e.GET("/cp/login", actionCpLogin).Name = actionNameCpLogin
	e.POST("/cp/login", actionCpLoginSubmit).Name = actionNameCpLoginSubmit
	// e.GET("/cp/logout", actionCpLogout).Name = actionNameCpLogout
	e.GET("/cp", actionCpDashboard, middlewareRequiredAuth).Name = actionNameCpDashboard

	return nil
}

func initDaos() {
	sqlc = newSqliteConnection("./data/sqlite", namespace)
	sqliteInitTableGroup(sqlc, tableGroup)
	sqliteInitTableUser(sqlc, tableUser)

	groupDao = newGroupDaoSqlite(sqlc, tableGroup)
	systemGroup, err := groupDao.Get(SystemGroupId)
	if err != nil {
		panic("error while getting group [" + SystemGroupId + "]: " + err.Error())
	}
	if systemGroup == nil {
		log.Printf("System group [%s] not found, creating one...", SystemGroupId)
		result, err := groupDao.Create(SystemGroupId, "System User Group")
		if err != nil {
			panic("error while creating group [" + SystemGroupId + "]: " + err.Error())
		}
		if !result {
			log.Printf("Cannot create group [%s]", SystemGroupId)
		}
	}

	userDao = newUserDaoSqlite(sqlc, tableUser)
	adminUser, err := userDao.Get(AdminUserUsernname)
	if err != nil {
		panic("error while getting user [" + AdminUserUsernname + "]: " + err.Error())
	}
	if adminUser == nil {
		pwd := "s3cr3t"
		log.Printf("Admin user [%s] not found, creating one with password [%s]...", AdminUserUsernname, pwd)
		result, err := userDao.Create(AdminUserUsernname, encryptPassword(AdminUserUsernname, pwd), AdminUserUsernname, SystemGroupId)
		if err != nil {
			panic("error while creating user [" + AdminUserUsernname + "]: " + err.Error())
		}
		if !result {
			log.Printf("Cannot create user [%s]", AdminUserUsernname)
		}
	}
}

/*----------------------------------------------------------------------*/
func newTemplateRenderer(directory, templateFileSuffix string) *myRenderer {
	return &myRenderer{
		directory:          directory,
		templateFileSuffix: templateFileSuffix,
		templates:          map[string]*template.Template{},
	}
}

// myRenderer is a custom html/template renderer for Echo framework
// See: https://echo.labstack.com/guide/templates
type myRenderer struct {
	directory          string
	templateFileSuffix string
	templates          map[string]*template.Template
}

/*
Render renders a template document

	- name is list of template names, separated by colon (e.g. <template-name-1>[:<template-name-2>[:<template-name-3>...]])
*/
func (r *myRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	v := reflect.ValueOf(data)
	if data == nil || v.IsNil() {
		data = make(map[string]interface{})
	}

	sess := getSession(c)
	flash := sess.Flashes()
	sess.Save(c.Request(), c.Response())

	// add global data/methods if data is a map
	if viewContext, isMap := data.(map[string]interface{}); isMap {
		viewContext["cdn_mode"] = cdnMode
		viewContext["static"] = myStaticPath
		viewContext["i18n"] = myI18n
		viewContext["reverse"] = c.Echo().Reverse
		viewContext["appInfo"] = goadmin.AppConfig.GetConfig("app")
		if len(flash) > 0 {
			viewContext["flash"] = flash[0].(string)
		}
		uid := c.Get(sessionMyUid)
		if uid != nil {
			viewContext["uid"] = uid
		}
	}

	tpl := r.templates[name]
	tokens := strings.Split(name, ":")
	if tpl == nil {
		var files []string
		for _, v := range tokens {
			files = append(files, r.directory+"/"+v+r.templateFileSuffix)
		}
		tpl = template.Must(template.New(name).ParseFiles(files...))
		r.templates[name] = tpl
	}
	return tpl.ExecuteTemplate(w, tokens[0]+".html", data)
}

/*----------------------------------------------------------------------*/
// authentication middleware
func middlewareRequiredAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		sess := getSession(c)

		uid, has := sess.Values[sessionMyUid]
		if has {
			uid, _ = reddo.ToString(uid)
		}
		if uid == nil || uid == "" {
			return c.Redirect(http.StatusFound, c.Echo().Reverse(actionNameCpLogin))
		}

		// ssoToken, has := sess.Values[sessionMySsoToken]
		// if has {
		// 	ssoToken, _ = reddo.ToString(ssoToken)
		// }
		// if ssoToken == nil || ssoToken == "" {
		// 	return c.Redirect(http.StatusFound, c.Echo().Reverse(actionNameCpLogin))
		// }
		//
		// if now := time.Now(); now.Unix()-lastSessionCheck.Unix() > 5*60 {
		// 	// do not verify sso-token so often, 5 mins sleep should be ok
		// 	lastSessionCheck = now
		//
		// 	app, err := model.LoadBoApp(SystemAppId)
		// 	if err != nil {
		// 		return c.String(http.StatusOK, I18n.Text("error_db_201", SystemAppId+"/"+err.Error()))
		// 	}
		// 	if app == nil {
		// 		return c.String(http.StatusOK, I18n.Text("error_app_not_found", SystemAppId))
		// 	}
		// 	if !app.IsConfigOk() {
		// 		return c.String(http.StatusOK, I18n.Text("error_app_invalid_config", SystemAppId))
		// 	}
		// 	if !app.IsActive() {
		// 		return c.String(http.StatusForbidden, I18n.Text("error_app_inactive", SystemAppId))
		// 	}
		//
		// 	ssoData, err := DecodeSsoData(app, ssoToken.(string))
		// 	if err != nil {
		// 		return renderGhnSsoCallbackForm(c, app, "", err.Error(), c.Echo().Reverse(actionNameCpLogin)+"?r="+util.RandomString(8))
		// 	}
		// 	if !ssoData.IsValid() || ssoData.IsExpired() {
		// 		return renderGhnSsoCallbackForm(c, app, "", I18n.Text("error_session_invalid_or_expired", ssoData.SsoId), c.Echo().Reverse(actionNameCpLogin)+"?r="+util.RandomString(8))
		// 	}
		// }
		//
		// c.Set(sessionMyUid, uid)
		return next(c)
	}
}

func actionHome(c echo.Context) error {
	return c.Render(http.StatusOK, namespace+":landing", nil)
}

func actionCpLogin(c echo.Context) error {
	return c.Render(http.StatusOK, namespace+":login", nil)
}

func actionCpLoginSubmit(c echo.Context) error {
	const (
		formFieldUsername = "username"
		formFieldPassword = "password"
	)
	var username, password, encPassword string
	var user *User
	var errMsg string
	var err error
	formData, err := c.FormParams()
	if err != nil {
		errMsg = myI18n.Text("error_form_400", err.Error())
		goto end
	}
	username = formData.Get(formFieldUsername)
	user, err = userDao.Get(username)
	if err != nil {
		errMsg = myI18n.Text("error_db_001", err.Error())
		goto end
	}
	if user == nil {
		errMsg = myI18n.Text("error_user_not_found", username)
		goto end
	}
	password = formData.Get(formFieldPassword)
	encPassword = encryptPassword(user.Username, password)
	if encPassword != user.Password {
		errMsg = myI18n.Text("error_login_failed")
		goto end
	}

	// login successful
	setSessionValue(c, sessionMyUid, user.Username)
	return c.Redirect(http.StatusFound, c.Echo().Reverse(actionNameCpDashboard))
end:
	return c.Render(http.StatusOK, namespace+":login", map[string]interface{}{
		"form":  formData,
		"error": errMsg,
	})
}

func actionCpDashboard(c echo.Context) error {
	return c.Render(http.StatusOK, namespace+":layout:cp_dashboard", map[string]interface{}{
		"active": "dashboard",
	})
}