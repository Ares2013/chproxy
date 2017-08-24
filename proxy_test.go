package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/hagen1778/chproxy/config"
	"github.com/hagen1778/chproxy/log"
	"bytes"
)

func TestMain(m *testing.M) {
	log.SuppressOutput(true)
	retCode := m.Run()
	log.SuppressOutput(false)
	os.Exit(retCode)
}

var goodCfg = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "cluster",
			Scheme: "http",
			Nodes:  []string{"localhost:8123"},
			OutUsers: []config.OutUser{
				{
					Name: "web",
				},
			},
		},
	},
	GlobalUsers: []config.GlobalUser{
		{
			Name:      "default",
			ToCluster: "cluster",
			ToUser:    "web",
		},
	},
}

func TestNewReverseProxy(t *testing.T) {
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if len(proxy.clusters) != 1 {
		t.Fatalf("got %d hosts; expected: %d", len(proxy.clusters), 1)
	}

	c := proxy.clusters["cluster"]
	if len(c.hosts) != 1 {
		t.Fatalf("got %d hosts; expected: %d", len(c.hosts), 1)
	}

	if c.hosts[0].addr.Host != "localhost:8123" {
		t.Fatalf("got %s host; expected: %s", c.hosts[0].addr.Host, "localhost:8123")
	}

	if len(proxy.users) != 1 {
		t.Fatalf("got %d users; expected: %d", len(proxy.users), 1)
	}

	if _, ok := proxy.users["default"]; !ok {
		t.Fatalf("expected user %q to be present in users", "default")
	}
}

var badCfg = &config.Config{
	Clusters: []config.Cluster{
		{
			Name:   "badCfg",
			Scheme: "udp",
			Nodes:  []string{"localhost:8123"},
			OutUsers: []config.OutUser{
				{
					Name: "default",
				},
			},
		},
	},
	GlobalUsers: []config.GlobalUser{
		{
			Name:      "default",
			ToCluster: "cluster",
			ToUser:    "default",
		},
	},
}

func TestApplyConfig(t *testing.T) {
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if err = proxy.ApplyConfig(badCfg); err == nil {
		t.Fatalf("error expected; got nil")
	}

	if _, ok := proxy.clusters["badCfg"]; ok {
		t.Fatalf("bad config applied; expected previous config")
	}
}

var fakeServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}
	r.Body.Close()

	d, err := time.ParseDuration(string(body))
	if err != nil {
		fmt.Fprintln(w, "Err")
		return
	}

	time.Sleep(d)
	fmt.Fprintln(w, "Ok")
}))

func makeRequest(p *reverseProxy) string {
	return makeHeavyRequest(p, time.Duration(0))
}

func makeHeavyRequest(p *reverseProxy, duration time.Duration) string {
	body := bytes.NewBufferString(duration.String())
	req := httptest.NewRequest("POST", fakeServer.URL, body)
	rw := httptest.NewRecorder()
	p.ServeHTTP(rw, req)
	resp := rw.Result()
	response, _ := ioutil.ReadAll(resp.Body)

	return string(response)
}

func TestReverseProxy_ServeHTTP(t *testing.T) {
	addr, err := url.Parse(fakeServer.URL)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	goodCfg.Clusters[0].Nodes = []string{addr.Host}
	proxy, err := NewReverseProxy(goodCfg)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	t.Run("Ok response", func(t *testing.T) {
		expected := "Ok\n"
		resp := makeRequest(proxy)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})

	t.Run("max concurrent queries for execution user", func(t *testing.T) {
		proxy.clusters["cluster"].users["web"].maxConcurrentQueries = 1
		go makeHeavyRequest(proxy, time.Millisecond * 20)
		time.Sleep(time.Millisecond * 10)

		expected := "limits for execution user \"web\" are exceeded: maxConcurrentQueries limit: 1"
		resp := makeRequest(proxy)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
		proxy.clusters["cluster"].users["web"].maxConcurrentQueries = 0
	})

	time.Sleep(time.Millisecond * 50)
	t.Run("max execution time for execution user", func(t *testing.T) {
		proxy.clusters["cluster"].users["web"].maxExecutionTime = time.Millisecond * 10

		expected := "timeout for execution user \"web\" exceeded: 10ms"
		resp := makeHeavyRequest(proxy, time.Millisecond * 20)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
		proxy.clusters["cluster"].users["web"].maxExecutionTime = 0
	})

	//t.Run("max concurrent queries for initial user", func(t *testing.T) {
	//	proxy.users["default"].maxConcurrentQueries = 1
	//	go makeHeavyRequest(proxy, time.Millisecond * 20)
	//	time.Sleep(time.Millisecond * 10)
	//
	//	expected := "limits for initial user \"default\" are exceeded: maxConcurrentQueries limit: 1"
	//	resp := makeRequest(proxy)
	//	if resp != expected {
	//		t.Fatalf("expected response: %q; got: %q", expected, resp)
	//	}
	//})

	time.Sleep(time.Millisecond * 50)
	t.Run("max execution time for initial user", func(t *testing.T) {
		proxy.users["default"].maxExecutionTime = time.Millisecond * 10

		expected := "timeout for initial user \"default\" exceeded: 10ms"
		resp := makeHeavyRequest(proxy, time.Millisecond * 20)
		if resp != expected {
			t.Fatalf("expected response: %q; got: %q", expected, resp)
		}
	})
	time.Sleep(time.Millisecond*20)
}


