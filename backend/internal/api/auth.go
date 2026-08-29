package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"it-platform/backend/internal/auth"
)

type LoginInput struct {
	Body struct {
		Email    string `json:"email" format:"email"`
		Password string `json:"password"`
	}
}

type LoginOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Email string `json:"email"`
	}
}

type LogoutInput struct {
	SessionToken string `cookie:"itp_session"`
}

type LogoutOutput struct {
	SetCookie string `header:"Set-Cookie"`
	Body      struct {
		Success bool `json:"success"`
	}
}

type MeInput struct {
	SessionToken string `cookie:"itp_session"`
}

type MeOutput struct {
	Body struct {
		Email string `json:"email"`
	}
}

func registerAuth(api huma.API, s *Server) {
	huma.Register(api, huma.Operation{
		OperationID: "login",
		Method:      "POST",
		Path:        "/api/auth/login",
		Summary:     "Log in as an admin",
	}, func(ctx context.Context, in *LoginInput) (*LoginOutput, error) {
		token, err := s.Auth.Login(ctx, in.Body.Email, in.Body.Password)
		if err != nil {
			if err == auth.ErrInvalidCredentials {
				return nil, huma.Error401Unauthorized("invalid email or password")
			}
			return nil, huma.Error500InternalServerError("login failed", err)
		}
		out := &LoginOutput{}
		out.SetCookie = (&http.Cookie{
			Name:     sessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(7 * 24 * time.Hour),
		}).String()
		out.Body.Email = in.Body.Email
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "logout",
		Method:      "POST",
		Path:        "/api/auth/logout",
		Summary:     "Log out",
	}, func(ctx context.Context, in *LogoutInput) (*LogoutOutput, error) {
		_ = s.Auth.Logout(ctx, in.SessionToken)
		out := &LogoutOutput{}
		out.SetCookie = (&http.Cookie{
			Name:     sessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Unix(0, 0),
		}).String()
		out.Body.Success = true
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      "GET",
		Path:        "/api/auth/me",
		Summary:     "Get the current logged-in admin",
	}, func(ctx context.Context, in *MeInput) (*MeOutput, error) {
		email, err := s.requireAuth(ctx, in.SessionToken)
		if err != nil {
			return nil, err
		}
		out := &MeOutput{}
		out.Body.Email = email
		return out, nil
	})
}
