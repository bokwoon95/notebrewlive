package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/bokwoon95/notebrew"
	"github.com/bokwoon95/notebrew/sq"
	"github.com/stripe/stripe-go/v79"
	portalsession "github.com/stripe/stripe-go/v79/billingportal/session"
	"github.com/stripe/stripe-go/v79/checkout/session"
	"github.com/stripe/stripe-go/v79/webhook"
)

func stripeCheckout(nbrew *notebrew.Notebrew, w http.ResponseWriter, r *http.Request, user User, stripeConfig StripeConfig) {
	if r.Method != "POST" {
		nbrew.MethodNotAllowed(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20 /* 1 MB */)
	err := r.ParseForm()
	if err != nil {
		nbrew.BadRequest(w, r, err)
		return
	}
	priceID := r.Form.Get("priceID")
	if priceID == "" {
		nbrew.BadRequest(w, r, fmt.Errorf("priceID not provided"))
		return
	}
	valid := false
	for _, plan := range stripeConfig.Plans {
		if plan.PriceID == priceID {
			valid = true
			break
		}
	}
	if !valid {
		nbrew.BadRequest(w, r, fmt.Errorf("invalid priceID"))
		return
	}
	scheme := "https://"
	if r.TLS == nil {
		scheme = "http://"
	}
	var customerID, email *string
	if user.CustomerID != "" {
		customerID = &user.CustomerID
	} else {
		email = &user.Email
	}
	expiresAt := time.Now().Add(30 * time.Minute)
	checkoutSession, err := session.New(&stripe.CheckoutSessionParams{
		Customer:      customerID,
		CustomerEmail: email,
		ExpiresAt:     stripe.Int64(expiresAt.Unix()),
		Mode:          stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{},
		SuccessURL:       stripe.String(scheme + nbrew.CMSDomain + "/stripe/checkout/success/?sessionID={CHECKOUT_SESSION_ID}"),
		CancelURL:        stripe.String(scheme + nbrew.CMSDomain + "/users/profile/"),
	})
	if err != nil {
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) {
			if stripeErr.Code == stripe.ErrorCodeResourceMissing {
				nbrew.BadRequest(w, r, fmt.Errorf("invalid customerID %q", user.CustomerID))
				return
			}
		}
		nbrew.GetLogger(r.Context()).Error(err.Error())
		nbrew.InternalServerError(w, r, err)
		return
	}
	http.Redirect(w, r, checkoutSession.URL, http.StatusSeeOther)
}

func stripeCheckoutSuccess(nbrew *notebrew.Notebrew, w http.ResponseWriter, r *http.Request, user User, stripeConfig StripeConfig) {
	if r.Method != "GET" && r.Method != "HEAD" {
		nbrew.MethodNotAllowed(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20 /* 1 MB */)
	err := r.ParseForm()
	if err != nil {
		nbrew.BadRequest(w, r, err)
		return
	}
	sessionID := r.Form.Get("sessionID")
	checkoutSession, err := session.Get(sessionID, &stripe.CheckoutSessionParams{
		Expand: stripe.StringSlice([]string{"line_items"}),
	})
	if err != nil {
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) {
			if stripeErr.Code == stripe.ErrorCodeResourceMissing {
				nbrew.BadRequest(w, r, fmt.Errorf("invalid sessionID %q", sessionID))
				return
			}
		}
		nbrew.GetLogger(r.Context()).Error(err.Error())
		nbrew.InternalServerError(w, r, err)
		return
	}
	if user.CustomerID == "" {
		_, err := sq.Exec(r.Context(), nbrew.DB, sq.Query{
			Dialect: nbrew.Dialect,
			Format:  "INSERT INTO customer (customer_id, user_id) VALUES ({customerID}, {userID})",
			Values: []any{
				sq.StringParam("customerID", checkoutSession.Customer.ID),
				sq.UUIDParam("userID", user.UserID),
			},
		})
		if err != nil {
			if nbrew.ErrorCode == nil {
				nbrew.GetLogger(r.Context()).Error(err.Error())
				nbrew.InternalServerError(w, r, err)
				return
			}
			errorCode := nbrew.ErrorCode(err)
			if !notebrew.IsKeyViolation(nbrew.Dialect, errorCode) {
				nbrew.GetLogger(r.Context()).Error(err.Error())
				nbrew.InternalServerError(w, r, err)
				return
			}
		}
	}
	var plan *Plan
	for _, lineItem := range checkoutSession.LineItems.Data {
		if lineItem.Price == nil {
			continue
		}
		for i := range stripeConfig.Plans {
			if stripeConfig.Plans[i].PriceID == lineItem.Price.ID {
				plan = &stripeConfig.Plans[i]
				break
			}
		}
		if plan != nil {
			break
		}
	}
	if plan != nil {
		b, err := json.Marshal(plan.UserFlags)
		if err != nil {
			nbrew.GetLogger(r.Context()).Error(err.Error())
			nbrew.InternalServerError(w, r, err)
			return
		}
		_, err = sq.Exec(r.Context(), nbrew.DB, sq.Query{
			Dialect: nbrew.Dialect,
			Format:  "UPDATE users SET site_limit = {siteLimit}, storage_limit = {storageLimit}, user_flags = {userFlags} WHERE user_id = {userID}",
			Values: []any{
				sq.Int64Param("siteLimit", plan.SiteLimit),
				sq.Int64Param("storageLimit", plan.StorageLimit),
				sq.Param("userFlags", sq.DialectExpression{
					Default: sq.Expr("json_patch(coalesce(user_flags, json_object()), {})", string(b)),
					Cases: []sq.DialectCase{{
						Dialect: "postgres",
						Result:  sq.Expr("coalesce(user_flags, jsonb_build_object()) || CAST({} AS JSONB)", string(b)),
					}, {
						Dialect: "mysql",
						Result:  sq.Expr("json_merge_patch(coalesce(user_flags, json_object(), CAST({} AS JSON)))", string(b)),
					}},
				}),
				sq.UUIDParam("userID", user.UserID),
			},
		})
		if err != nil {
			nbrew.GetLogger(r.Context()).Error(err.Error())
			nbrew.InternalServerError(w, r, err)
			return
		}
		err = nbrew.SetFlashSession(w, r, map[string]any{
			"postRedirectGet": map[string]any{
				"from":         "stripe/checkout/success",
				"siteLimit":    plan.SiteLimit,
				"storageLimit": plan.StorageLimit,
			},
		})
		if err != nil {
			nbrew.GetLogger(r.Context()).Error(err.Error())
			nbrew.InternalServerError(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/users/profile/", http.StatusSeeOther)
}

func stripePortal(nbrew *notebrew.Notebrew, w http.ResponseWriter, r *http.Request, user User) {
	if r.Method != "POST" {
		nbrew.MethodNotAllowed(w, r)
		return
	}
	if user.CustomerID == "" {
		nbrew.BadRequest(w, r, fmt.Errorf("user has no customerID"))
		return
	}
	scheme := "https://"
	if r.TLS == nil {
		scheme = "http://"
	}
	billingPortalSession, err := portalsession.New(&stripe.BillingPortalSessionParams{
		Customer:  stripe.String(user.CustomerID),
		ReturnURL: stripe.String(scheme + nbrew.CMSDomain + "/users/profile/"),
	})
	if err != nil {
		var stripeErr *stripe.Error
		if errors.As(err, &stripeErr) {
			if stripeErr.Code == stripe.ErrorCodeResourceMissing {
				nbrew.BadRequest(w, r, fmt.Errorf("invalid customerID %q", user.CustomerID))
				return
			}
		}
		nbrew.GetLogger(r.Context()).Error(err.Error())
		nbrew.InternalServerError(w, r, err)
		return
	}
	http.Redirect(w, r, billingPortalSession.URL, http.StatusSeeOther)
}

func stripeWebhook(nbrew *notebrew.Notebrew, w http.ResponseWriter, r *http.Request, stripeConfig StripeConfig) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20 /* 1 MB */))
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			nbrew.BadRequest(w, r, err)
			return
		}
		nbrew.GetLogger(r.Context()).Error(err.Error())
		nbrew.InternalServerError(w, r, err)
		return
	}
	event, err := webhook.ConstructEvent(b, r.Header.Get("Stripe-Signature"), stripeConfig.WebhookSecret)
	if err != nil {
		nbrew.BadRequest(w, r, err)
		return
	}
	switch event.Type {
	case "customer.subscription.created":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			nbrew.BadRequest(w, r, err)
			return
		}
		if subscription.Status != stripe.SubscriptionStatusActive {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var plan *Plan
		for _, subscriptionItem := range subscription.Items.Data {
			if subscriptionItem.Price == nil {
				continue
			}
			for i := range stripeConfig.Plans {
				if stripeConfig.Plans[i].PriceID == subscriptionItem.Plan.ID {
					plan = &stripeConfig.Plans[i]
					break
				}
			}
			if plan != nil {
				break
			}
		}
		if plan != nil {
			b, err := json.Marshal(plan.UserFlags)
			if err != nil {
				nbrew.GetLogger(r.Context()).Error(err.Error())
				nbrew.InternalServerError(w, r, err)
				return
			}
			_, err = sq.Exec(r.Context(), nbrew.DB, sq.Query{
				Dialect: nbrew.Dialect,
				Format: "UPDATE users" +
					" SET site_limit = {siteLimit}, storage_limit = {storageLimit}, user_flags = {userFlags}" +
					" WHERE user_id = (SELECT user_id FROM customer WHERE customer_id = {customerID})",
				Values: []any{
					sq.Int64Param("siteLimit", plan.SiteLimit),
					sq.Int64Param("storageLimit", plan.StorageLimit),
					sq.Param("userFlags", sq.DialectExpression{
						Default: sq.Expr("json_patch(coalesce(user_flags, json_object()), {})", string(b)),
						Cases: []sq.DialectCase{{
							Dialect: "postgres",
							Result:  sq.Expr("coalesce(user_flags, jsonb_build_object()) || CAST({} AS JSONB)", string(b)),
						}, {
							Dialect: "mysql",
							Result:  sq.Expr("json_merge_patch(coalesce(user_flags, json_object(), CAST({} AS JSON)))", string(b)),
						}},
					}),
					sq.StringParam("customerID", subscription.Customer.ID),
				},
			})
			if err != nil {
				nbrew.GetLogger(r.Context()).Error(err.Error())
				nbrew.InternalServerError(w, r, err)
				return
			}
		}
	case "customer.subscription.updated":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			nbrew.BadRequest(w, r, err)
			return
		}
		var plan *Plan
		if subscription.Status == stripe.SubscriptionStatusActive {
			if subscription.CancelAtPeriodEnd {
				// If we reach here it means that the customer canceled the
				// subscription, but it only kicks in at the end of the billing
				// period. Once that happens we will receive another webhook
				// event, this time where the subscription status ==
				// "canceled". So, there is nothing to do right now and the
				// customer gets to keep using their currently active
				// subscription (at least until the end of the billing period).
				w.WriteHeader(http.StatusNoContent)
				return
			}
			for _, subscriptionItem := range subscription.Items.Data {
				if subscriptionItem.Price == nil {
					continue
				}
				for i := range stripeConfig.Plans {
					if stripeConfig.Plans[i].PriceID == subscriptionItem.Plan.ID {
						plan = &stripeConfig.Plans[i]
						break
					}
				}
				if plan != nil {
					break
				}
			}
		} else {
			plan = &Plan{
				SiteLimit:    1,
				StorageLimit: 10_000_000,
				UserFlags: map[string]bool{
					"NoUploadImage":  true,
					"NoCustomDomain": true,
				},
			}
			for i := range stripeConfig.Plans {
				if stripeConfig.Plans[i].PriceID == "" {
					plan = &stripeConfig.Plans[i]
					break
				}
			}
		}
		if plan != nil {
			b, err := json.Marshal(plan.UserFlags)
			if err != nil {
				nbrew.GetLogger(r.Context()).Error(err.Error())
				nbrew.InternalServerError(w, r, err)
				return
			}
			_, err = sq.Exec(r.Context(), nbrew.DB, sq.Query{
				Dialect: nbrew.Dialect,
				Format: "UPDATE users" +
					" SET site_limit = {siteLimit}, storage_limit = {storageLimit}, user_flags = {userFlags}" +
					" WHERE user_id = (SELECT user_id FROM customer WHERE customer_id = {customerID})",
				Values: []any{
					sq.Int64Param("siteLimit", plan.SiteLimit),
					sq.Int64Param("storageLimit", plan.StorageLimit),
					sq.Param("userFlags", sq.DialectExpression{
						Default: sq.Expr("json_patch(coalesce(user_flags, json_object()), {})", string(b)),
						Cases: []sq.DialectCase{{
							Dialect: "postgres",
							Result:  sq.Expr("coalesce(user_flags, jsonb_build_object()) || CAST({} AS JSONB)", string(b)),
						}, {
							Dialect: "mysql",
							Result:  sq.Expr("json_merge_patch(coalesce(user_flags, json_object(), CAST({} AS JSON)))", string(b)),
						}},
					}),
					sq.StringParam("customerID", subscription.Customer.ID),
				},
			})
			if err != nil {
				nbrew.GetLogger(r.Context()).Error(err.Error())
				nbrew.InternalServerError(w, r, err)
				return
			}
		}
	case "customer.subscription.deleted":
		var subscription stripe.Subscription
		err := json.Unmarshal(event.Data.Raw, &subscription)
		if err != nil {
			nbrew.BadRequest(w, r, err)
			return
		}
		_, err = sq.Exec(r.Context(), nbrew.DB, sq.Query{
			Dialect: nbrew.Dialect,
			Format: "UPDATE users" +
				" SET site_limit = {siteLimit}, storage_limit = {storageLimit}" +
				" WHERE user_id = (SELECT user_id FROM customer WHERE customer_id = {customerID})",
			Values: []any{
				sq.Int64Param("siteLimit", 1),
				sq.Int64Param("storageLimit", 10_000_000),
				sq.StringParam("customerID", subscription.Customer.ID),
			},
		})
		if err != nil {
			nbrew.GetLogger(r.Context()).Error(err.Error())
			nbrew.InternalServerError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
