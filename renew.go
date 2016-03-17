package acmewrapper

import (
	"errors"
	"fmt"
	"time"

	"github.com/xenolf/lego/acme"
)

// http://stackoverflow.com/questions/15323767/does-golang-have-if-x-in-construct-similar-to-python
func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// checks if the two arrays of strings contain the same elements
func arraysMatch(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, i := range a {
		if !stringInSlice(i, b) {
			return false
		}
	}
	return true
}

// Renew generates a new certificate
func (w *AcmeWrapper) Renew() (err error) {
	w.configmutex.Lock()
	defer w.configmutex.Unlock()

	if w.config.AcmeDisabled {
		return errors.New("Can't renew cert when ACME is disabled")
	}

	// If a cert already exists, use the same private key. If it doesn't
	// then generate a new one
	w.certmutex.RLock()
	crt := w.cert
	w.certmutex.RUnlock()

	// TODO: In the future, figure out how to get renewals working with
	// the information we have
	cert, errmap := w.client.ObtainCertificate(w.config.Domains, true, nil)
	err = nil
	if len(errmap) != 0 {
		for _, errv := range errmap {
			if _, ok := errv.(acme.TOSError); ok {
				err = errv
			}
		}
		if err == nil {
			err = fmt.Errorf("%v", errmap)
		}

	}

	// If our error is a terms of service change, see if we accept it
	if _, ok := err.(acme.TOSError); ok {
		if !w.config.TOSCallback(w.registration.TosURL) {
			return errors.New("Did not accept new TOS")
		}

		err = w.client.AgreeToTOS()
		if err != nil {
			return err
		}

	} else {
		return err
	}

	if err != nil {
		// We agreed to new TOS. try again
		var errmap map[string]error
		cert, errmap = w.client.ObtainCertificate(w.config.Domains, true, nil)
		err = nil
		if len(errmap) != 0 {
			err = fmt.Errorf("%v", errmap)
		}
		// See if there was a new error
		if err != nil {
			return err
		}
	}

	crt, err = tlsCert(cert)
	if err != nil {
		return err
	}

	// Write the certs to file if we are using file-backed stuff
	if w.config.TLSCertFile != "" {
		writeCert(w.config.TLSCertFile, w.config.TLSKeyFile, cert)
	}

	w.certmutex.Lock()
	w.cert = crt
	w.certmutex.Unlock()
	return nil
}

// backgroundExpirationChecker is exactly that - it runs in the background
// and ensures that messages regarding certificate expiration as well as
// any renewals if ACME is configured are run on time.
func backgroundExpirationChecker(w *AcmeWrapper) {
	for {
		time.Sleep(time.Duration(w.config.RenewTime) * time.Second)
		if w.CertNeedsUpdate() {
			for {

				if w.config.RenewCallback != nil {
					w.config.RenewCallback()
				}
				if !w.config.AcmeDisabled {
					err := w.Renew()
					if err != nil && w.config.RenewFailedCallback != nil {
						w.config.RenewFailedCallback(err)
					}
				}
				if !w.CertNeedsUpdate() {
					break
				}
				time.Sleep(time.Duration(w.config.RetryDelay) * time.Second)
			}
		}

	}
}