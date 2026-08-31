package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPprofServerUsesLoopbackAndIndependentMux(t *testing.T) {
	server := newPprofServer()
	require.Equal(t, "127.0.0.1:8005", server.Addr)
	require.NotSame(t, http.DefaultServeMux, server.Handler)

	request := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
}
