package agent

import "testing"

func TestLooksLikeCronRequest(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{input: "制定一个14:35的定时任务", want: true},
		{input: "定时器测试：当前时间14:28，1分钟后推送", want: true},
		{input: "帮我查一下北京天气", want: false},
	}

	for _, tc := range cases {
		if got := looksLikeCronRequest(tc.input); got != tc.want {
			t.Fatalf("looksLikeCronRequest(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestLooksLikeCronSuccessReply(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{input: "## 🎯 **14:35定时任务已成功设置！**", want: true},
		{input: "Created job 'reminder' (id: abc123)", want: true},
		{input: "### 最终状态：\n- 任务ID：b8c3d7f4\n- 推送时间：21:26\n- 状态：✅ 任务已创建\n\n定时任务已创建，等待21:26推送。", want: true},
		{input: "我还没有创建任务，需要再确认一下时间。", want: false},
	}

	for _, tc := range cases {
		if got := looksLikeCronSuccessReply(tc.input); got != tc.want {
			t.Fatalf("looksLikeCronSuccessReply(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
