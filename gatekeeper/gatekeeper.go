// A distributed rate limit implementation for vulcand
package gatekeeper

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/mailgun/vulcand/Godeps/_workspace/src/github.com/codegangsta/cli"
	"github.com/mailgun/vulcand/plugin"
	"github.com/miniclip/ratelimit"
)

const Type = "gatekeeper"

// GatekeeperMiddleware struct holds configuration parameters and is used to
// serialize/deserialize the configuration from storage engines.
type GatekeeperMiddleware struct {
	Header   string
	Frontend string
	Keys     map[string]GatekeeperKey
}

type GatekeeperKey struct {
	Rate int64
}

type GatekeeperClient struct {
	LastSecondUsed uint64
	Bucket         *ratelimit.Bucket
}

// Auth middleware handler
type GatekeeperHandler struct {
	config GatekeeperMiddleware
	next   http.Handler
}

// API rate limiting response
type GatekeeperClientRateLimit struct {
	Error     string `json:"error"`
	Rate      uint64 `json:"rate"`
	Remaining uint64 `json:"remaining"`
}

type Configuration struct {
	Debug bool
	RateLimitPeriod int
	GatekeeperProtocol string
	GatekeeperHost string
	GatekeeperTimeout int
}

// A map of gatekeeper clients with their api key as the key and their status as the value
var clients = make(map[string]*GatekeeperClient)

// Once a second make a best-effort attempt to sync the data in a shared store
var ticker = time.NewTicker(time.Second * 1)

// Configuration map
var config = Configuration{
	Debug: true,
	RateLimitPeriod: 60, // period for rate limiting allocations in seconds
	GatekeeperProtocol: "http", // protocol to connect to the gatekeeper rate limiting api
	GatekeeperHost: "gatekeeper-host.com", // host for the gatekeeper rate limiting api
	GatekeeperTimeout: 500, // timeout for connecting to the gatekeeper rate limiting api
}

func GetSpec() *plugin.MiddlewareSpec {
	return &plugin.MiddlewareSpec{
		Type:      Type,       // A short name for the middleware
		FromOther: FromOther,  // Tells vulcand how to create middleware from another one (this is for deserialization)
		FromCli:   FromCli,    // Tells vulcand how to create middleware from command line tool
		CliFlags:  CliFlags(), // Vulcand will add these flags to middleware specific command line tool
	}
}

func (handler *GatekeeperHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get the clients API key
	key := r.Header.Get(handler.config.Header)

	// Check for the existence of the API Key in the Keys map
	if _, ok := handler.config.Keys[key]; ok {
		// Now attempt to take a token from that clients token bucket
		taken, remaining := clients[key].Bucket.TakeAvailable(1)

		// If were able to take a token then allow the request to continue
		if taken > 0 {
			// Increment the LastSecondUsed variable by 1
			atomic.AddUint64(&clients[key].LastSecondUsed, 1)

			// Set some useful headers regarding the rate limits
			w.Header().Set("X-Rate-Limit-Limit", fmt.Sprintf("%v", handler.config.Keys[key].Rate))
			w.Header().Set("X-Rate-Limit-Remaining", fmt.Sprintf("%v", remaining))

			handler.next.ServeHTTP(w, r)
			// Otherwise inform the client their request is not allowed
		} else {
			w.WriteHeader(429)
			io.WriteString(w, "Too many requests")
		}
		// Otherwise the request is unathorized
	} else {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "Unauthorized")
	}
}

// This function is optional but handy, it's used to check input parameters when creating new middlewares
func New(header string, frontend string, keys map[string]GatekeeperKey) (*GatekeeperMiddleware, error) {
	if header == "" {
		return nil, fmt.Errorf("A header must be specified")
	}

	if len(keys) < 1 {
		return nil, fmt.Errorf("At least one API key must be specified")
	}

	// Add an entry for each of the clients to the rate limiting system
	for key, meta := range keys {
		// Calculate the fill interval for this client
		fillInterval := time.Duration(config.RateLimitPeriod) * time.Second

		// Get the capacity and fill amount for the bucket (they are the same in this implementation)
		capacity := meta.Rate
		fillAmount := meta.Rate

		clients[key] = &GatekeeperClient{
			Bucket: ratelimit.NewBucketWithQuantum(fillInterval, capacity, fillAmount),
		}
	}

	// Every second sync the rate limiting stats with the other gateway server(s)
	go func() {
		for range ticker.C {
			for key, _ := range keys {
				client := clients[key]

				// The current time
				now := time.Now()

				// Adjust the buckets stats before we do anything
				client.Bucket.Adjust(now)

				// Extract a copy of the last second used
				lastSecondUsed := atomic.LoadUint64(&client.LastSecondUsed)

				// Log the last second used
				if (config.Debug) {
					fmt.Println(fmt.Sprintf("[LAST SECOND USED] %v", lastSecondUsed))
				}

				// Log the current second used
				if (config.Debug) {
					fmt.Println(fmt.Sprintf("[BUCKET USED] %v", client.Bucket.Used()))
				}

				// Generate the url for the rate limiting server request
				requestUrl := fmt.Sprintf("%s://%s/v1/frontends/%s/clients/%s", config.GatekeeperProtocol, config.GatekeeperHost, frontend, key)

				// Create our http client
				httpClient := http.Client{
					Timeout: time.Duration(config.GatekeeperTimeout) * time.Millisecond,
				}

				// Post to the rate limiting server our Values
				response, err := httpClient.PostForm(requestUrl, url.Values{"usage": {fmt.Sprintf("%v", lastSecondUsed)}})

				// If no errors occured then try and read in the response body
				if err == nil {
					// Read in the JSON response
					jsonResponse, err := ioutil.ReadAll(response.Body)

					// Again provided no errors occured then try and parse the JSON
					if err == nil {
						rateLimitResponse := &GatekeeperClientRateLimit{}
						err = json.Unmarshal([]byte(jsonResponse), &rateLimitResponse)

						// Finally if on errors occured in the JSON unmarshalling subtract
						// the global usage minus our own from the bucket
						if err == nil {
							// Did an error occur in the API request
							if rateLimitResponse.Error == "" && response.StatusCode == http.StatusOK {
								// Set the amount remaining in the bucket
								client.Bucket.SetAvailable(int64(rateLimitResponse.Remaining))
							} else {
								// TODO we should probably log this or do something here
								fmt.Println(fmt.Sprintf("Status: %v, Response: %s", response.StatusCode, string(jsonResponse)))
							}
						}
					}
				}

				// Reset the LastSecondUsed counter to 0
				atomic.StoreUint64(&client.LastSecondUsed, 0)
			}
		}
	}()

	return &GatekeeperMiddleware{
		Header:   header,
		Frontend: frontend,
		Keys:     keys,
	}, nil
}

// This function is important, it's called by vulcand to create a new handler from the middleware config
// and put it into the middleware chain.
func (middleware *GatekeeperMiddleware) NewHandler(next http.Handler) (http.Handler, error) {
	return &GatekeeperHandler{
		next:   next,
		config: *middleware,
	}, nil
}

// String() will be called by loggers inside Vulcand and command line tool.
func (middleware *GatekeeperMiddleware) String() string {
	return fmt.Sprintf("header=%v, keys=%v", middleware.Header, "TODO")
}

// FromOther Will be called by vulcand when the engine or API read the middleware from the serialized format.
// It's important that the signature of the function is exactly the same, otherwise Vulcand will
// fail to register this middleware. The first and the only parameter should be the struct itself, no pointers and
// other variables. The function should return a middleware interface and error in case if the parameters are wrong.
func FromOther(middleware GatekeeperMiddleware) (plugin.Middleware, error) {
	return New(middleware.Header, middleware.Frontend, middleware.Keys)
}

// FromCli constructs the middleware from the command line
func FromCli(c *cli.Context) (plugin.Middleware, error) {
	// TODO, make this work..
	return New(c.String("header"), c.String("frontend"), make(map[string]GatekeeperKey))
}

// CliFlags will be used by Vulcand construct help and CLI command for the vctl command
func CliFlags() []cli.Flag {
	return []cli.Flag{
		cli.StringFlag{
			"header, H",
			"",
			"the http header to use for authentication",
			"",
		},
		cli.StringFlag{
			"frontend, F",
			"",
			"the frontend id",
			"",
		},
		cli.StringFlag{
			"keys, K",
			"",
			"api keys in a spaceless csv format",
			"",
		},
	}
}
