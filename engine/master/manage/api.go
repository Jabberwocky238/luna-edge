package manage

import "net/http"

// API 暴露 repository manage REST API。
type API struct {
	wrapper *Wrapper
}

// NewAPI 创建 API。
func NewAPI(wrapper *Wrapper) *API {
	return &API{wrapper: wrapper}
}

// Handler 返回聚合后的 http handler。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealthz)
	mux.HandleFunc("/manage/", a.handleResource)
	return mux
}

func (a *API) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
