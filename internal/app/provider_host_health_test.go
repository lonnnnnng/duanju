package app

import "testing"

func TestProviderHostHealthOrdersFailedHostLast(t *testing.T) {
	health := newProviderHostHealth()
	a := "https://a.example.test/x"
	b := "https://b.example.test/x"
	c := "https://c.example.test/x"

	health.report(b, true, 100_000_000) // 100ms
	health.report(a, false, 0)

	ordered := health.order([]string{a, c, b})
	if ordered[0] != b {
		t.Fatalf("期望已成功且更快的主机排第一，实际 %v", ordered)
	}
	if ordered[len(ordered)-1] != a {
		t.Fatalf("期望失败主机排最后，实际 %v", ordered)
	}
}

func TestProviderHostHealthRecoversAfterSuccess(t *testing.T) {
	health := newProviderHostHealth()
	address := "https://cdn.example.test/video"
	health.report(address, false, 0)
	if cooldown, _ := health.score(address); !cooldown {
		t.Fatal("失败后应进入冷却")
	}
	health.report(address, true, 50_000_000)
	if cooldown, _ := health.score(address); cooldown {
		t.Fatal("成功请求后应解除冷却")
	}
}

func TestProviderHostHealthOrderMediaAddresses(t *testing.T) {
	health := newProviderHostHealth()
	good := "https://good.example.test/a.mp4"
	bad := "https://bad.example.test/a.mp4"
	health.report(bad, false, 0)

	ordered := health.orderMediaAddresses([]string{bad, "", good, good})
	if len(ordered) != 2 || ordered[0] != good || ordered[1] != bad {
		t.Fatalf("备用地址未按健康度去重排序: %v", ordered)
	}
}
