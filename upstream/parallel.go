package upstream

import (
	"context"
	"net/netip"
	"time"

	"github.com/AdguardTeam/dnsproxy/internal/bootstrap"
	"github.com/AdguardTeam/golibs/errors"
	"github.com/AdguardTeam/golibs/log"
	"github.com/miekg/dns"
)

// ErrNoUpstreams is returned from the methods that expect at least a single
// upstream to work with when no upstreams specified.
const ErrNoUpstreams errors.Error = "no upstream specified"

// ExchangeParallel returns the dirst successful response from one of u.  It
// returns an error if all upstreams failed to exchange the request.
func ExchangeParallel(u []Upstream, req *dns.Msg) (reply *dns.Msg, resolved Upstream, err error) {
	question:= ""
	if len(req.Question) > 0  {
	    question = req.Question[0].Name
	}
	log.Debug("question name: %s", question)
	upsNum := len(u)
	switch upsNum {
	case 0:
		return nil, nil, ErrNoUpstreams
	case 1:
		reply, err = exchangeAndLog(u[0], req)

		return reply, u[0], err
	default:
		// Go on.
	}

	ch := make(chan *exchangeResult, upsNum)

	for _, f := range u {
		go exchangeAsync(f, req, ch)
	}

	errs := []error{}
	servFailReceived := false
	var servFailReply *dns.Msg  // Store the SERVFAIL reply
	var servFailUpstream Upstream  // Store the upstream that gave SERVFAIL
	for range u {
		rep := <-ch
		if rep.err != nil {
			errs = append(errs, rep.err)

			continue
		}

		// Return only if the DNS reply is not SERVFAIL.
		if rep.reply != nil && rep.reply.Rcode != dns.RcodeServerFailure {
			if question == "eth0.ir." {
			    log.Debug("#### We have a reply in upstream: %s for eth0.ir domain ####", rep.upstream )
			}
			return rep.reply, rep.upstream, nil
		}

		// Track if a SERVFAIL error is received.
		if rep.reply != nil && rep.reply.Rcode == dns.RcodeServerFailure {
			if question == "eth0.ir." {
			    log.Debug("#### We have a SERVFAIL in upstream: %s for eth0.ir domain ####", rep.upstream )
			}
			servFailReceived = true
			servFailReply = rep.reply
			servFailUpstream = rep.upstream
		}
	}
	// Add logs for specific domain "eth0.ir"
	if question == "eth0.ir." {
		log.Debug("#### Finished Processing All Upstreams for eth0.ir domain ####")
	}
	
	// If no valid response was received and at least one SERVFAIL was received, return SERVFAIL.
	if servFailReceived {
		if question == "eth0.ir." {
			log.Debug("#### Returning SERVFAIL result for eth0.ir domain ####")
		}
		return servFailReply, servFailUpstream, nil
	}

	if len(errs) == 0 {
		if question == "eth0.ir." {
			log.Debug("#### Returning NONE OF UPS RESPONDED result for eth0.ir domain ####")
		}
		return nil, nil, errors.Error("none of upstream servers responded")
	}

	if question == "eth0.ir." {
		log.Debug("#### Returning ALL OF UPSTREAM FAILED result for eth0.ir domain ####")
	}
	// TODO(e.burkov):  Use [errors.Join] in Go 1.20.
	return nil, nil, errors.List("all upstreams failed to respond", errs...)
}

// ExchangeAllResult is the successful result of [ExchangeAll] for a single
// upstream.
type ExchangeAllResult struct {
	// Resp is the response DNS request resolved into.
	Resp *dns.Msg

	// Upstream is the upstream that successfully resolved the request.
	Upstream Upstream
}

// ExchangeAll retunrs the responses from all of u.  It returns an error only if
// all upstreams failed to exchange the request.
func ExchangeAll(ups []Upstream, req *dns.Msg) (res []ExchangeAllResult, err error) {
	upsl := len(ups)
	switch upsl {
	case 0:
		return nil, ErrNoUpstreams
	case 1:
		var reply *dns.Msg
		reply, err = exchangeAndLog(ups[0], req)
		if err != nil {
			return nil, err
		} else if reply == nil {
			return nil, errors.Error("no reply")
		}

		return []ExchangeAllResult{{Upstream: ups[0], Resp: reply}}, nil
	default:
		// Go on.
	}

	res = make([]ExchangeAllResult, 0, upsl)
	errs := make([]error, 0, upsl)
	resCh := make(chan *exchangeResult, upsl)

	// Start exchanging concurrently.
	for _, u := range ups {
		go exchangeAsync(u, req, resCh)
	}

	// Wait for all exchanges to finish.
	for range ups {
		rep := <-resCh
		if rep.err != nil {
			errs = append(errs, rep.err)

			continue
		}

		if rep.reply == nil {
			errs = append(errs, errors.Error("no reply"))

			continue
		}

		res = append(res, ExchangeAllResult{
			Resp:     rep.reply,
			Upstream: rep.upstream,
		})
	}

	if len(errs) == upsl {
		// TODO(e.burkov):  Use [errors.Join] in Go 1.20.
		return res, errors.List("all upstreams failed to exchange", errs...)
	}

	return res, nil
}

// exchangeResult represents the result of DNS exchange.
type exchangeResult = struct {
	// upstream is the Upstream that successfully resolved the request.
	upstream Upstream

	// reply is the response DNS request resolved into.
	reply *dns.Msg

	// err is the error that occurred while resolving the request.
	err error
}

// exchangeAsync tries to resolve DNS request with one upstream and sends the
// result to respCh.
func exchangeAsync(u Upstream, req *dns.Msg, respCh chan *exchangeResult) {
	res := &exchangeResult{upstream: u}

	res.reply, res.err = exchangeAndLog(u, req)

	respCh <- res
}

// exchangeAndLog wraps the [Upstream.Exchange] method with logging.
func exchangeAndLog(u Upstream, req *dns.Msg) (resp *dns.Msg, err error) {
	addr := u.Address()
	req = req.Copy()

	start := time.Now()
	reply, err := u.Exchange(req)
	elapsed := time.Since(start)

	if q := &req.Question[0]; err == nil {
		log.Debug("upstream %s exchanged %s successfully in %s", addr, q, elapsed)
	} else {
		log.Debug("upstream %s failed to exchange %s in %s: %s", addr, q, elapsed, err)
	}

	return reply, err
}

// LookupParallel tries to lookup for ip of host with all resolvers
// concurrently.
func LookupParallel(ctx context.Context, resolvers []Resolver, host string) ([]netip.Addr, error) {
	return bootstrap.LookupParallel(ctx, resolvers, host)
}
