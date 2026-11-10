package main

import (
	"context"
	"time"

	"github.com/egoist/mygo"
)

func protectWindowClose(win *mygo.Window) {
	checking := false
	win.OnClose(func(event *mygo.CloseEvent) {
		if checking {
			event.PreventDefault()
			return
		}
		checking = true
		defer func() { checking = false }()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		reason, err := win.Page().EvalContext(ctx, `window.xsourcetreePrepareClose?.() ?? "ready"`)
		allow := err == nil && reason == "ready"
		switch {
		case err != nil:
			result, dialogErr := mygo.Dialog.Message(mygo.MessageOptions{
				Parent: win, Type: mygo.MessageWarning, Message: "无法确认窗口中的操作是否完成",
				Detail:  "退出或重新启动可能中断操作并丢失未保存的草稿。",
				Buttons: []string{"取消", "仍然退出"}, DefaultButton: 0, CancelButton: 0,
				DestructiveButtons: []int{1},
			})
			allow = dialogErr == nil && result.Button == 1
		case reason == "busy":
			_, _ = mygo.Dialog.Message(mygo.MessageOptions{
				Parent: win, Type: mygo.MessageInfo, Message: "当前操作尚未完成",
				Detail:  "请等待 Git 操作、扫描或刷新完成后，再退出或重新启动。",
				Buttons: []string{"好"},
			})
		case reason == "drafts":
			result, dialogErr := mygo.Dialog.Message(mygo.MessageOptions{
				Parent: win, Type: mygo.MessageWarning, Message: "要丢弃未保存的草稿并退出吗？",
				Detail:  "退出或重新启动将丢失提交说明和冲突编辑草稿，磁盘上的仓库文件不会因此改变。",
				Buttons: []string{"取消", "丢弃草稿并退出"}, DefaultButton: 0, CancelButton: 0,
				DestructiveButtons: []int{1},
			})
			allow = dialogErr == nil && result.Button == 1
		}
		if !allow {
			event.PreventDefault()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _ = win.Page().EvalContext(ctx, `window.xsourcetreeCancelClose?.()`)
		}
	})
}
