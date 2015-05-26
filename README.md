# Vulcand Gatekeeper

An authentication and [distributed rate limiting](https://github.com/miniclip/gatekeeper) [middleware](https://docs.vulcand.io/middlewares.html) for [vulcand](https://github.com/mailgun/vulcand).

## Building

Create a directory in your GOPATH to be used with your version of vulcand that contains the bundled middleware.

`mkdir -p ${GOPATH}/src/github.com/miniclip/vulcand-gatekeeper`

`cd` into the that directory and initialise the middleware using the `vbundle` command.

`vbundle init --middleware=github.com/miniclip/vulcand-gatekeeper/gatekeeper`

You can now build a new version of the vulcand program with our middleware included like so:

`go build -o vulcand`

Following this we need to build a new version of `vctl` also:

`pushd vctl/ && go build -o vctl && pop`

To test it compiled correctly run `./vctl/vctl gatekeeper --help` and you should see a help menu.

## Configuration

You can configure the middleware (i.e specify the header to use add API keys for a frontend) via etcd, an example using `etcdctl` is shown below:

`etcdctl set /vulcand/frontends/f1/middlewares/gatekeeper '{"Type": "gatekeeper", "Middleware":{"Header": "X-API-Key", "Frontend": "f1", "Keys": {"abc": {"Rate": 2}}}}'`

## Roadmap

- [ ] HMAC based request signatures not just API keys
- [ ] Use GoDep for dependencies
- [ ] Tests
- [ ] Improved documentation on how to deploy with vulcand

## License

The MIT License (MIT)

Copyright (c) 2015 Miniclip

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
