// Package sub provides subscription server functionality for the 3x-ui panel,
// including HTTP/HTTPS servers for serving subscription links and JSON configurations.
package sub

import (
	"context"
	"crypto/tls"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SawaMEN/3x-ui/v3/internal/logger"
	"github.com/SawaMEN/3x-ui/v3/internal/util/common"
	"github.com/SawaMEN/3x-ui/v3/internal/web/locale"
	"github.com/SawaMEN/3x-ui/v3/internal/web/network"
	"github.com/SawaMEN/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Server represents the subscription server that serves subscription links and JSON configurations.
type Server struct {
	httpServer *http.Server
	routerMu   sync.RWMutex
	listener   net.Listener

	sub            *SUBController
	settingService service.SettingService

	ctx    context.Context
	cancel context.CancelFunc
}

// NewServer creates a new subscription server instance with a cancellable context.
func NewServer() *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		ctx:    ctx,
		cancel: cancel,
	}
}

// initRouter configures the subscription server's Gin engine, middleware,
// templates and static assets and returns the ready-to-use engine.
func (s *Server) initRouter() (*gin.Engine, error) {
	// Always run in release mode for the subscription server
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	gin.SetMode(gin.ReleaseMode)

	engine := gin.Default()

	subDomain, err := s.settingService.GetSubDomain()
	if err != nil {
		return nil, err
	}

	if subDomain != "" {
		engine.Use(s.subscriptionDomainValidator(subDomain))
	}

	LinksPath, err := s.settingService.GetSubPath()
	if err != nil {
		return nil, err
	}

	JsonPath, err := s.settingService.GetSubJsonPath()
	if err != nil {
		return nil, err
	}

	ClashPath, err := s.settingService.GetSubClashPath()
	if err != nil {
		return nil, err
	}

	subJsonEnable, err := s.settingService.GetSubJsonEnable()
	if err != nil {
		return nil, err
	}

	subClashEnable, err := s.settingService.GetSubClashEnable()
	if err != nil {
		return nil, err
	}

	subClashAutoDetect, err := s.settingService.GetSubClashAutoDetect()
	if err != nil {
		subClashAutoDetect = false
	}

	subJsonAutoDetect, err := s.settingService.GetSubJsonAutoDetect()
	if err != nil {
		subJsonAutoDetect = false
	}

	subJsonAlwaysArray, err := s.settingService.GetSubJsonAlwaysArray()
	if err != nil {
		subJsonAlwaysArray = false
	}

	subJsonUserAgentRegex, err := s.settingService.GetSubJsonUserAgentRegex()
	if err != nil {
		subJsonUserAgentRegex = service.DefaultSubJsonUserAgentRegex
	}

	subClashUserAgentRegex, err := s.settingService.GetSubClashUserAgentRegex()
	if err != nil {
		subClashUserAgentRegex = service.DefaultSubClashUserAgentRegex
	}

	// Set base_path based on LinksPath for template rendering
	// Ensure LinksPath ends with "/" for proper asset URL generation
	basePath := LinksPath
	if basePath != "/" && !strings.HasSuffix(basePath, "/") {
		basePath += "/"
	}
	// logger.Debug("sub: Setting base_path to:", basePath)
	engine.Use(func(c *gin.Context) {
		c.Set("base_path", basePath)
	})

	Encrypt, err := s.settingService.GetSubEncrypt()
	if err != nil {
		return nil, err
	}

	RemarkTemplate, err := s.settingService.GetRemarkTemplate()
	if err != nil {
		RemarkTemplate = ""
	}

	SubUpdates, err := s.settingService.GetSubUpdates()
	if err != nil {
		SubUpdates = "10"
	}

	SubJsonMux, err := s.settingService.GetSubJsonMux()
	if err != nil {
		SubJsonMux = ""
	}

	SubJsonRules, err := s.settingService.GetSubJsonRules()
	if err != nil {
		SubJsonRules = ""
	}

	SubJsonRoutingRules, err := s.settingService.GetSubJsonRoutingRules()
	if err != nil {
		SubJsonRoutingRules = ""
	}

	SubJsonDns, err := s.settingService.GetSubJsonDns()
	if err != nil {
		SubJsonDns = ""
	}

	SubJsonFinalMask, err := s.settingService.GetSubJsonFinalMask()
	if err != nil {
		SubJsonFinalMask = ""
	}

	SubJsonObservatory, err := s.settingService.GetSubJsonObservatory()
	if err != nil {
		SubJsonObservatory = ""
	}

	SubClashEnableRouting, err := s.settingService.GetSubClashEnableRouting()
	if err != nil {
		SubClashEnableRouting = false
	}

	SubClashRules, err := s.settingService.GetSubClashRules()
	if err != nil {
		SubClashRules = ""
	}

	SubTitle, err := s.settingService.GetSubTitle()
	if err != nil {
		SubTitle = ""
	}

	SubSupportUrl, err := s.settingService.GetSubSupportUrl()
	if err != nil {
		SubSupportUrl = ""
	}

	SubProfileUrl, err := s.settingService.GetSubProfileUrl()
	if err != nil {
		SubProfileUrl = ""
	}
	SubProfileMode, err := s.settingService.GetSubProfileMode()
	if err != nil {
		SubProfileMode = service.SubProfileModeNone
	}

	SubAnnounce, err := s.settingService.GetSubAnnounce()
	if err != nil {
		SubAnnounce = ""
	}

	SubEnableRouting, err := s.settingService.GetSubEnableRouting()
	if err != nil {
		return nil, err
	}

	SubRoutingRules, err := s.settingService.GetSubRoutingRules()
	if err != nil {
		SubRoutingRules = ""
	}

	SubHideSettings, err := s.settingService.GetSubHideSettings()
	if err != nil {
		SubHideSettings = false
	}

	SubIncyEnableRouting, err := s.settingService.GetSubIncyEnableRouting()
	if err != nil {
		SubIncyEnableRouting = false
	}

	SubIncyRoutingRules, err := s.settingService.GetSubIncyRoutingRules()
	if err != nil {
		SubIncyRoutingRules = ""
	}

	happCfg := HappConfig{}
	happCfg.AutoDetect, _ = s.settingService.GetSubHappAutoDetect()
	happCfg.ProviderId, _ = s.settingService.GetSubHappProviderId()
	happCfg.NewUrl, _ = s.settingService.GetSubHappNewUrl()
	happCfg.FallbackUrl, _ = s.settingService.GetSubHappFallbackUrl()
	happCfg.SubInfoColor, _ = s.settingService.GetSubHappSubInfoColor()
	happCfg.SubInfoText, _ = s.settingService.GetSubHappSubInfoText()
	happCfg.SubInfoButtonText, _ = s.settingService.GetSubHappSubInfoButtonText()
	happCfg.SubInfoButtonLink, _ = s.settingService.GetSubHappSubInfoButtonLink()
	happCfg.SubExpire, _ = s.settingService.GetSubHappSubExpire()
	happCfg.SubExpireButtonLink, _ = s.settingService.GetSubHappSubExpireButtonLink()
	happCfg.NotificationExpire, _ = s.settingService.GetSubHappNotificationExpire()
	happCfg.NoLimit, _ = s.settingService.GetSubHappNoLimit()
	happCfg.AlwaysHwid, _ = s.settingService.GetSubHappAlwaysHwid()
	happCfg.TunMode, _ = s.settingService.GetSubHappTunMode()
	happCfg.TunType, _ = s.settingService.GetSubHappTunType()
	happCfg.ExcludeRoutes, _ = s.settingService.GetSubHappExcludeRoutes()
	happCfg.ExcludeApns, _ = s.settingService.GetSubHappExcludeApns()
	happCfg.ColorProfile, _ = s.settingService.GetSubHappColorProfile()
	happCfg.PingType, _ = s.settingService.GetSubHappPingType()
	happCfg.AutoConnect, _ = s.settingService.GetSubHappAutoConnect()
	happCfg.AutoConnectType, _ = s.settingService.GetSubHappAutoConnectType()
	happCfg.PerAppMode, _ = s.settingService.GetSubHappPerAppMode()
	happCfg.PerAppList, _ = s.settingService.GetSubHappPerAppList()

	// set per-request localizer from headers/cookies
	engine.Use(locale.LocalizerMiddleware())

	// Mount the Vite-built dist/assets/ so the subscription page's JS/CSS
	// bundles load from `/assets/...`. Also mount the same FS under the
	// subscription path prefix (LinksPath + "assets") so reverse proxies
	// running the panel under a URI prefix can resolve those URLs too.
	// Note: LinksPath always starts and ends with "/" (validated in settings).
	var linksPathForAssets string
	if LinksPath == "/" {
		linksPathForAssets = "/assets"
	} else {
		linksPathForAssets = strings.TrimRight(LinksPath, "/") + "/assets"
	}

	var assetsFS http.FileSystem
	if _, err := os.Stat("internal/web/dist/assets"); err == nil {
		assetsFS = http.FS(os.DirFS("internal/web/dist/assets"))
	} else if subFS, err := fs.Sub(distFS, "dist/assets"); err == nil {
		assetsFS = http.FS(subFS)
	} else {
		logger.Error("sub: failed to mount embedded dist assets:", err)
	}

	if assetsFS != nil {
		engine.StaticFS("/assets", assetsFS)
		if linksPathForAssets != "/assets" {
			engine.StaticFS(linksPathForAssets, assetsFS)
		}

		// Browser may resolve subpage assets relative to the request URL —
		// /sub/<basePath>/<subId>/assets/... — so route those to the same FS.
		if LinksPath != "/" {
			engine.Use(func(c *gin.Context) {
				path := c.Request.URL.Path
				pathPrefix := strings.TrimRight(LinksPath, "/") + "/"
				if strings.HasPrefix(path, pathPrefix) && strings.Contains(path, "/assets/") {
					_, after, ok := strings.Cut(path, "/assets/")
					if ok {
						assetPath := after // +8 to skip "/assets/"
						if assetPath != "" {
							c.FileFromFS(assetPath, assetsFS)
							c.Abort()
							return
						}
					}
				}
				c.Next()
			})
		}

		// Legacy Hiddify pages must keep their JS/CSS requests below the imported
		// proxy_path_client prefix. These assets are served by the subscription
		// engine only for requests internally forwarded by the public panel listener;
		// exposing them directly on subPort would make the Hiddify page available
		// through the generic subscription listener again.
		engine.Use(func(c *gin.Context) {
			if isLegacyHiddifyPanelForward(c.Request) {
				aliases, err := s.settingService.GetHiddifyLegacySubscriptionAliases()
				if err == nil {
					if assetPath, ok := legacyHiddifyAssetPath(c.Request.URL.Path, aliases); ok {
						c.FileFromFS(assetPath, assetsFS)
						c.Abort()
						return
					}
				}
			}
			c.Next()
		})
	}

	g := engine.Group("/")

	s.sub = NewSUBController(g,
		WithSUBPath(LinksPath),
		WithSUBJsonPath(JsonPath),
		WithSUBClashPath(ClashPath),
		WithSUBClashAutoDetect(subClashAutoDetect),
		WithSUBClashUserAgentRegex(subClashUserAgentRegex),
		WithSUBJsonAutoDetect(subJsonAutoDetect),
		WithSUBJsonUserAgentRegex(subJsonUserAgentRegex),
		WithSUBJsonAlwaysArray(subJsonAlwaysArray),
		WithSUBJsonEnabled(subJsonEnable),
		WithSUBClashEnabled(subClashEnable),
		WithSUBEncryption(Encrypt),
		WithSUBRemarkTemplate(RemarkTemplate),
		WithSUBUpdateInterval(SubUpdates),
		WithSUBJsonMux(SubJsonMux),
		WithSUBJsonRules(SubJsonRules),
		WithSUBJsonRoutingRules(SubJsonRoutingRules),
		WithSUBJsonDns(SubJsonDns),
		WithSUBJsonFinalMask(SubJsonFinalMask),
		WithSUBJsonObservatory(SubJsonObservatory),
		WithSUBClashEnableRouting(SubClashEnableRouting),
		WithSUBClashRules(SubClashRules),
		WithSUBTitle(SubTitle),
		WithSUBSupportURL(SubSupportUrl),
		WithSUBProfileURL(SubProfileUrl),
		WithSUBProfileMode(SubProfileMode),
		WithSUBAnnounce(SubAnnounce),
		WithSUBEnableRouting(SubEnableRouting),
		WithSUBRoutingRules(SubRoutingRules),
		WithSUBHideSettings(SubHideSettings),
		WithSUBHappConfig(happCfg),
		WithSUBIncyEnableRouting(SubIncyEnableRouting),
		WithSUBIncyRoutingRules(SubIncyRoutingRules),
	)
	registerTelemtSubscriptionRoute(g)

	// Keep the compatibility handler registered so the panel can dispatch an
	// imported Hiddify URL into this engine in-process. The handler itself rejects
	// external requests that arrive directly on the generic subscription listener.
	engine.NoRoute(s.legacyHiddifySubscription)

	return engine, nil
}

type legacyHiddifyPanelForwardKey struct{}

func markLegacyHiddifyPanelForward(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), legacyHiddifyPanelForwardKey{}, true))
}

func isLegacyHiddifyPanelForward(r *http.Request) bool {
	if r == nil {
		return false
	}
	forwarded, _ := r.Context().Value(legacyHiddifyPanelForwardKey{}).(bool)
	return forwarded
}

func (s *Server) subscriptionDomainValidator(primary string) gin.HandlerFunc {
	primary = normalizeRequestHost(primary)
	return func(c *gin.Context) {
		host := normalizeRequestHost(c.Request.Host)
		if host == primary {
			c.Next()
			return
		}

		// A legacy Hiddify URL may intentionally use its imported public domain,
		// but only when the main panel listener already validated and forwarded it.
		// Direct requests on subPort must obey the normal subscription domain rule.
		if isLegacyHiddifyPanelForward(c.Request) {
			aliases, err := s.settingService.GetHiddifyLegacySubscriptionAliases()
			if err == nil {
				if subID, ok := legacyHiddifySubID(c.Request.URL.Path, aliases); ok && s.settingService.IsHiddifySubscriptionPath(subID, c.Request.URL.Path) {
					c.Next()
					return
				}
				if _, ok := legacyHiddifyAssetPath(c.Request.URL.Path, aliases); ok {
					c.Next()
					return
				}
			}
		}

		c.AbortWithStatus(http.StatusForbidden)
	}
}

func normalizeRequestHost(host string) string {
	host = strings.TrimSpace(host)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	} else {
		host = strings.Trim(host, "[]")
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func (s *Server) legacyHiddifySubscription(c *gin.Context) {
	// The Hiddify compatibility URL belongs to the public panel/web listener,
	// not to the generic subscription listener. Serve it only after the panel
	// has explicitly forwarded the validated request in-process.
	if !isLegacyHiddifyPanelForward(c.Request) {
		c.Status(http.StatusNotFound)
		return
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		c.Status(http.StatusNotFound)
		return
	}

	aliases, err := s.settingService.GetHiddifyLegacySubscriptionAliases()
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	subID, ok := legacyHiddifySubID(c.Request.URL.Path, aliases)
	if !ok || !s.settingService.IsHiddifySubscriptionPath(subID, c.Request.URL.Path) {
		c.Status(http.StatusNotFound)
		return
	}

	// Keep default SPA assets below /<proxy_path_client>/ so the surrounding
	// reverse proxy continues routing them to the subscription listener.
	c.Set("base_path", legacyHiddifyBasePath(c.Request.URL.Path, subID))
	c.AddParam("subid", subID)
	s.sub.subs(c)
}

func legacyHiddifyBasePath(requestPath, subID string) string {
	path := strings.TrimRight(requestPath, "/")
	if subID == "" || !strings.HasSuffix(path, "/"+subID) {
		return "/"
	}
	base := strings.TrimSuffix(path, subID)
	if !strings.HasPrefix(base, "/") || !strings.HasSuffix(base, "/") {
		return "/"
	}
	return base
}

func legacyHiddifyAssetPath(requestPath string, aliases []service.HiddifyLegacySubscriptionAlias) (string, bool) {
	for _, alias := range aliases {
		path := strings.Trim(strings.TrimSpace(alias.Path), "/")
		if path == "" {
			continue
		}
		prefix := "/" + path + "/assets/"
		if !strings.HasPrefix(requestPath, prefix) {
			continue
		}
		assetPath := strings.TrimPrefix(requestPath, prefix)
		if assetPath == "" || !fs.ValidPath(assetPath) {
			continue
		}
		return assetPath, true
	}
	return "", false
}

func legacyHiddifySubID(requestPath string, aliases []service.HiddifyLegacySubscriptionAlias) (string, bool) {
	for _, alias := range aliases {
		path := strings.Trim(strings.TrimSpace(alias.Path), "/")
		if path == "" {
			continue
		}
		prefix := "/" + path + "/"
		if !strings.HasPrefix(requestPath, prefix) {
			continue
		}

		tail := strings.TrimPrefix(requestPath, prefix)
		tail = strings.TrimSuffix(tail, "/")
		if tail == "" || strings.Contains(tail, "/") {
			continue
		}
		if _, err := uuid.Parse(tail); err != nil {
			continue
		}
		return tail, true
	}
	return "", false
}

// ServeLegacySubscription lets the panel's public domain serve migrated URLs
// when its reverse proxy points to the panel listener rather than the separate
// subscription listener. Imported alias assets are forwarded as well so the
// browser page stays functional outside the dedicated subscription listener.
func (s *Server) ServeLegacySubscription(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	aliases, err := s.settingService.GetHiddifyLegacySubscriptionAliases()
	if err != nil {
		return false
	}
	if subID, ok := legacyHiddifySubID(r.URL.Path, aliases); !ok || !s.settingService.IsHiddifySubscriptionPath(subID, r.URL.Path) {
		if _, assetOK := legacyHiddifyAssetPath(r.URL.Path, aliases); !assetOK {
			return false
		}
	}
	s.routerMu.RLock()
	var handler http.Handler
	if s.httpServer != nil {
		handler = s.httpServer.Handler
	}
	s.routerMu.RUnlock()
	if handler == nil {
		return false
	}
	handler.ServeHTTP(w, markLegacyHiddifyPanelForward(r))
	return true
}

// Start initializes and starts the subscription server with configured settings.
func (s *Server) Start() (err error) {
	// This is an anonymous function, no function name
	defer func() {
		if err != nil {
			_ = s.Stop()
		}
	}()

	subEnable, err := s.settingService.GetSubEnable()
	if err != nil {
		return err
	}
	if !subEnable {
		return nil
	}

	engine, err := s.initRouter()
	if err != nil {
		return err
	}

	certFile, err := s.settingService.GetSubCertFile()
	if err != nil {
		return err
	}
	keyFile, err := s.settingService.GetSubKeyFile()
	if err != nil {
		return err
	}
	listen, err := s.settingService.GetSubListen()
	if err != nil {
		return err
	}
	port, err := s.settingService.GetSubPort()
	if err != nil {
		return err
	}

	listenAddr := net.JoinHostPort(listen, strconv.Itoa(port))
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", listenAddr)
	if err != nil {
		return err
	}

	if certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err == nil {
			c := &tls.Config{
				Certificates: []tls.Certificate{cert},
			}
			listener = network.NewAutoHttpsListener(listener)
			listener = tls.NewListener(listener, c)
			logger.Info("Sub server running HTTPS on", listener.Addr())
		} else {
			logger.Error("Error loading certificates:", err)
			logger.Info("Sub server running HTTP on", listener.Addr())
		}
	} else {
		logger.Info("Sub server running HTTP on", listener.Addr())
	}
	s.listener = listener

	s.routerMu.Lock()
	s.httpServer = &http.Server{
		Handler: engine,
		// The subscription server is the most exposed (public) listener; without
		// these a few slow-header connections exhaust it (Slowloris). Mirrors the
		// panel server timeouts in internal/web/web.go.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.routerMu.Unlock()

	go network.ServeHTTP(s.httpServer, listener, "Subscription server")

	return nil
}

// Stop gracefully shuts down the subscription server and closes the listener.
func (s *Server) Stop() error {
	s.cancel()

	var err1 error
	var err2 error
	if s.httpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		err1 = s.httpServer.Shutdown(shutdownCtx)
	}
	if s.listener != nil {
		err2 = s.listener.Close()
	}
	return common.Combine(err1, err2)
}

// GetCtx returns the server's context for cancellation and deadline management.
func (s *Server) GetCtx() context.Context {
	return s.ctx
}
