package Web

import (
	. "System/Config"
	"System/Routing"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"reflect"
	"strings"
)

type HttpHandler struct {
}

func (this *HttpHandler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("URL:" + r.URL.String() + "\t" + fmt.Sprintf("%v", err))
			if App.Configs.ShowErrors {
				this.Show505(rw, err)
			}
		}
	}()
	//解析请求
	contentType := r.Header.Get("Content-Type")
	enctype, _, _ := mime.ParseMediaType(contentType)
	r.ParseForm()
	if enctype == "multipart/form-data" {
		var size int64
		size = 30 * 1024 * 1024
		if (AppConfig.UploadSize + 3*1024*1024) > size {
			//如果上传文件大小限制超过30M，则在AppConfig.UploadSize上+3M做为ParseMultipartForm的大小
			size = AppConfig.UploadSize + 3*1024*1024
		}
		r.ParseMultipartForm(size) //配置文件默认30M
	}
	//获取请求路径
	requestPath := strings.Trim(r.URL.Path, "/")
	//判断请示的是否静态文件
	if this.ProcessStatic(requestPath, rw, r) {
		return
	}
	routeData := Routing.RouteTable.GetRouteData(requestPath)
	//路由匹配失败
	if routeData == nil {
		//404页面不存在
		this.Show404(rw, "")
		return
	}
	//创建 Controller
	ctl, err := App.GetController(routeData)
	//没有对应的Controller,或Action
	_, ok := routeData["area"]
	var strArea string
	if ok {
		strArea = fmt.Sprintf("%v", routeData["area"])
	}
	if err != nil {
		if err == ControllerNotExist || err == ActionNotExist {
			this.Show404(rw, strArea)
		} else {
			panic(err)
		}
		return
	}
	strMethodName := routeData["action"].(string)
	method := ctl.MethodByName(strMethodName)
	//Action不存在，这个情况应该不存在
	if !method.IsValid() {
		//404页面不存在
		this.Show404(rw, strArea)
		return
	}
	//获取Session
	sessions, err := App.SessionProvider.StartSession(rw, r, App.Configs.SessionLocation)
	if err != nil {
		App.Log.AddError(err)
	}
	//获取 cookies
	cookies := this.GetCookie(r)

	binder := NewBinder(r, routeData)
	//转换成IController接口
	controller := ctl.Interface()
	ictl := controller.(IController)
	//初始化Controller对像
	this.initController(ictl, rw, r, sessions, routeData, cookies, binder)

	//调用OnLoad函数
	this.CallOnLoad(ctl)
	//判断OnLoad中是否调用了EndResponse
	if ictl.IsEnd() {
		this.EndRequest(sessions, cookies, rw, r)
		return
	}
	//准备调用Action函数
	methodType := method.Type()
	//获取Action的参数
	param := this.GetMethodParam(methodType, binder)
	//调用Action函数
	result := method.Call(param)
	//判断是否调用了EndResponse
	if ictl.IsEnd() {
		this.EndRequest(sessions, cookies, rw, r)
		return
	}
	//将结果展现到前端
	if result != nil && len(result) > 0 {
		actionResult := result[0].Interface()
		iactionResult, ok := actionResult.(IActionResult)
		if ok {
			err = iactionResult.ExecuteResult()
			if err != nil {
				panic(err)
			}
		} else {
			//判断是否为String类型，如果是则直接输出该字符串
			iactionResult, ok := actionResult.(string)
			if ok {
				rw.Write([]byte(iactionResult))
			} else {
				//如果不是字符串类型，则转换成字符串类型进行输出
				strResult := fmt.Sprintf("%v", actionResult)
				rw.Write([]byte(strResult))
			}
		}

	}
	//调用UnLoad
	this.CallUnLoad(ctl)
	//调用EndSession
	App.SessionProvider.EndSession(sessions, App.Configs.SessionLocation, r)
}

//初始化Controller
func (this *HttpHandler) initController(ictl IController, rw http.ResponseWriter, r *http.Request, session map[string]interface{}, routData map[string]interface{}, cookies map[string]string, binder *Binder) {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler initController URL:" + r.URL.String() + "\t" + err.Error())
		}
	}()
	ictl.SetResponse(rw)
	ictl.SetRequest(r)
	ictl.SetRouteData(routData)
	viewData := make(map[string]interface{})
	viewData["Request"] = r
	viewData["Controller"] = routData["controller"]
	viewData["Action"] = routData["action"]
	viewData["Theme"] = App.Configs.Theme
	viewData["Area"] = routData["area"]

	ictl.SetViewData(viewData)
	ictl.SetSession(session)
	ictl.SetTheme(App.Configs.Theme)
	ictl.SetViewEngin(App.ViewEngine)
	ictl.SetCookies(cookies)
	ictl.SetBinder(binder)
	ictl.SetQueryString(this.GetQueryString(r, routData))
	ictl.SetForm(this.GetForms(r))
	if r.Method == "POST" {
		ictl.SetIsPost(true)
	} else {
		ictl.SetIsPost(false)
	}
	//获取来源地址
	ictl.SetReferer(r.Header.Get("Referer"))
	//是否ajax请求，是返回true,否返回false
	if r.Header.Get("X-Requested-With") != "" {
		ictl.SetIsAjax(true)
	} else {
		ictl.SetIsAjax(false)
	}

}

/*调用OnLoad函数,如果存在*/
func (this *HttpHandler) CallOnLoad(ctl reflect.Value) {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler CallOnLoad\t" + err.Error())
		}
	}()
	onload := ctl.MethodByName("OnLoad")
	if !onload.IsValid() {
		//不存在直接返回true
		return
	}
	if onload.Type().NumIn() > 0 {
		//OnLoad不接受任何参数
		panic("OnLoad不能有任何参数")
	}
	onload.Call(nil)
}

/*调用UnLoad函数*/
func (this *HttpHandler) CallUnLoad(ctl reflect.Value) {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler CallUnLoad \t" + err.Error())
		}
	}()
	unload := ctl.MethodByName("UnLoad")
	if !unload.IsValid() {
		//不存在直接返回true
		return
	}
	if unload.Type().NumIn() > 0 {
		//UnLoad不接受任何参数
		panic("UnLoad不能有任何参数")
	}
	unload.Call(nil)
}

/*获取函数的参数*/
func (this *HttpHandler) GetMethodParam(methodType reflect.Type, binder *Binder) []reflect.Value {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler.GetMethodParam \t" + err.Error())

		}
	}()
	var param []reflect.Value
	if methodType.NumIn() > 0 {
		for i, j := 0, methodType.NumIn(); i < j; i++ {
			pt := methodType.In(i)
			//只能接受Struct类型的参数
			if pt.Kind() != reflect.Struct {

			}
			p := reflect.New(pt)
			err := binder.BindModel(p)
			if err != nil {
				panic(err)
			}
			p = p.Elem()
			param = append(param, p)
		}
	}
	return param
}
func (this *HttpHandler) ProcessStatic(requestPath string, w http.ResponseWriter, r *http.Request) bool {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("In HttpHandler.ProcessStatic:\t" + err.Error())
		}
	}()
	//转换为小写，不区分大小写的比较
	strLowerPath := strings.ToLower(requestPath)
	//判断是否静态文件
	if AppConfig.StaticFiles != nil {
		for _, v := range AppConfig.StaticFiles {
			if strLowerPath == v.Url {
				strFileName := v.FilePath
				http.ServeFile(w, r, strFileName)
				return true
			}
		}
	}
	//判断是否静态目录
	if AppConfig.StaticDir != nil {
		for _, v := range AppConfig.StaticDir {
			tem := strings.ToLower(v)
			tem = strings.Trim(tem, "/")
			if strings.HasPrefix(strLowerPath, tem) {
				strFileName := v + requestPath[len(v):]
				http.ServeFile(w, r, strFileName)
				return true
			}
		}
	}
	return false
}
func (this *HttpHandler) GetCookie(r *http.Request) map[string]string {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("In HttpHandler.GetCookie URL:" + r.URL.String() + "\t" + err.Error())
		}
	}()
	m := make(map[string]string)
	for _, v := range r.Cookies() {
		m[v.Name], _ = url.QueryUnescape(v.Value)
	}
	return m
}
func (this *HttpHandler) GetForms(r *http.Request) map[string]string {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler GetForms URL:" + r.URL.String() + "\t" + err.Error())
		}
	}()
	m := make(map[string]string)
	for k, v := range r.PostForm {
		m[k] = v[len(v)-1]
	}
	return m
}
func (this *HttpHandler) GetQueryString(r *http.Request, routData map[string]interface{}) map[string]string {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler GetQueryString URL:" + r.URL.String() + "\t" + err.Error())
		}
	}()
	m := make(map[string]string)
	//将路由参数存到QueryString里，方便程序统一处理
	for k, v := range routData {
		strKey := strings.TrimSpace(strings.ToLower(k))
		switch strKey {
		case "area":
			continue
		case "action":
			continue
		case "controller":
			continue
		}
		m[k] = fmt.Sprintf("%v", v)
	}
	//地址栏中的参数将覆盖同名的routeDate值
	querys := r.URL.Query()
	for k, v := range querys {
		m[k] = v[len(v)-1]
	}

	return m
}

//请求结束时，保存Session,设置cookies
func (this *HttpHandler) EndRequest(sessions map[string]interface{}, cookies map[string]string, rw http.ResponseWriter, r *http.Request) {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler EndRequest URL:" + r.URL.String() + "\t" + err.Error())
		}
	}()
	App.SessionProvider.EndSession(sessions, App.Configs.SessionLocation, r)
	for k, v := range cookies {
		v = url.QueryEscape(v)
		cookie := &http.Cookie{
			Name:     k,
			Value:    v,
			Path:     "/",
			HttpOnly: true,
		}
		if App.Configs.CookieDomain != "" {
			cookie.Domain = App.Configs.CookieDomain
		}
		http.SetCookie(rw, cookie)
	}
}

//显示404页面
func (this *HttpHandler) Show404(w http.ResponseWriter, strArea string) {
	defer func() {
		//错误处理
		if e := recover(); e != nil {
			err := e.(error)
			App.Log.AddErrMsg("in HttpHandler Show404\t" + err.Error())
		}
	}()
	viewData := make(map[string]interface{})
	viewData["area"] = strArea
	result := ViewResult{
		ViewData:       viewData,
		ViewEngine:     App.ViewEngine,
		Response:       w,
		ActionName:     "404",
		ControllerName: "",
		Theme:          App.Configs.Theme}

	err := result.ExecuteResult()
	if err != nil {
		strErr := "HttpHandler.Show404,页面展示时出错:" + err.Error()
		App.Log.Add(strErr)
		w.Write([]byte(strErr))
	}
}

//显示错误信息
func (this *HttpHandler) Show505(w http.ResponseWriter, err error) {

	viewData := make(map[string]interface{})
	errMsg := fmt.Sprintf("%v", err)
	viewData["ErrMsg"] = errMsg

	result := ViewResult{
		ViewData:       viewData,
		ViewEngine:     App.ViewEngine,
		Response:       w,
		ActionName:     "505",
		ControllerName: "",
		Theme:          App.Configs.Theme}

	err = result.ExecuteResult()
	if err != nil {
		strErr := "HttpHandler.Show505,页面展示时出错:" + err.Error()
		App.Log.Add(strErr)
		w.Write([]byte(strErr))
	}
}
