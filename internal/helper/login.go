package helper

import (
	"cmp"
	"context"
	"strings"
)

type loginParams struct {
	Session    *Session `json:"session"`
	Pair       *Pair    `json:"pair"`
	DeviceName string   `json:"device_name"`
}

type loginResult struct {
	User User `json:"user"`
}

const defaultDeviceName = "Empty"

func login(ctx context.Context, c *call) (any, error) {
	var p loginParams
	if err := c.decode(&p); err != nil {
		return nil, err
	}
	s := c.server
	request, err := s.loginFor(p)
	if err != nil {
		return nil, err
	}
	if err := s.beginLogin(); err != nil {
		return nil, err
	}
	run := &loginRun{}
	whatsapp, err := s.opts.Connect(ctx, request, s.emitterFor(run))
	if err != nil {
		s.endLogin(nil, nil)
		return nil, err
	}
	s.endLogin(whatsapp, run)
	return loginResult{User: whatsapp.Self()}, nil
}

func (s *server) loginFor(p loginParams) (Login, error) {
	if p.Session != nil && p.Session.Device != "" {
		return Login{Session: p.Session}, nil
	}
	if p.Pair != nil {
		pair, err := checkPair(*p.Pair)
		if err != nil {
			return Login{}, err
		}
		return Login{Pair: &pair, DeviceName: cmp.Or(strings.TrimSpace(p.DeviceName), defaultDeviceName)}, nil
	}
	if saved, ok := s.loadState(); ok && saved.Device != "" {
		return Login{Session: &saved}, nil
	}
	return Login{}, Errorf(CodeBadRequest, "The login needs either the stored session or a way to link a new device: pair by qr or by code.")
}

func checkPair(pair Pair) (Pair, error) {
	switch pair.Method {
	case PairByQR:
		return Pair{Method: PairByQR}, nil
	case PairByCode:
		phone, err := internationalNumber(pair.Phone)
		if err != nil {
			return Pair{}, err
		}
		return Pair{Method: PairByCode, Phone: phone}, nil
	default:
		return Pair{}, Errorf(CodeBadRequest, "Linking works by qr or by code.")
	}
}

func internationalNumber(phone string) (string, error) {
	digits := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9':
			return r
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
			return -1
		default:
			return 'x'
		}
	}, phone)
	if strings.ContainsRune(digits, 'x') || len(digits) < 7 || len(digits) > 15 || strings.HasPrefix(digits, "0") {
		return "", Errorf(CodeBadPhone, "Linking by code needs the phone number in international form, such as +46 70 123 45 67.")
	}
	return digits, nil
}

func (s *server) beginLogin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.whatsapp != nil || s.loggingIn {
		return Errorf(CodeAlreadyLoggedIn, "Someone is already logged in. Log out first.")
	}
	s.loggingIn = true
	return nil
}

func (s *server) endLogin(whatsapp WhatsApp, run *loginRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loggingIn = false
	s.whatsapp, s.run = whatsapp, run
}

func logout(ctx context.Context, c *call) (any, error) {
	s := c.server
	s.mu.Lock()
	current := s.whatsapp == c.whatsapp
	if current {
		s.whatsapp, s.run = nil, nil
	}
	s.mu.Unlock()
	if !current {
		return nil, Errorf(CodeNotLoggedIn, "Nobody is logged in yet. Log in first.")
	}
	if err := c.whatsapp.Logout(ctx); err != nil {
		return nil, err
	}
	if s.opts.StateDir != "" {
		s.forgetState()
	}
	return nil, nil
}
