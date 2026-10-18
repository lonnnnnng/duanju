package app

import "context"

type sourceChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var sourceChoices = []sourceChoice{{ID: "hongguo", Name: "主站"}}

func (app *UIApp) imageSource(_ context.Context, remote string) (string, bool) {
	app.mu.Lock()
	defer app.mu.Unlock()
	// long: 封面代理只接受剧库已登记的地址，避免局域网页面被用来请求任意远端资源。
	for _, drama := range app.dramas {
		if address, valid := buildImageURL(bestDramaCover(drama)); valid && address == remote {
			return dramaProvider(drama), true
		}
	}
	return "", false
}
