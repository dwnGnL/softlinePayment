package softlinePayment

import (
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

type Service struct {
	config *Config
}

const (
	auth          = "/v1/login_check"
	createPayment = "/v1/payment"
	makePayment   = "/v1/payment/recurring"
	getPayment    = "v1/order/"
	refund        = "/v1/order/%s/refund"
)

func New(config *Config) *Service {
	return &Service{
		config: config,
	}
}

func (s *Service) Auth() (response *AuthResp, err error) {
	response = new(AuthResp)

	// отправка в SOM
	body := new(bytes.Buffer)
	if err = json.NewEncoder(body).Encode(AuthReq{
		Username: s.config.Login,
		Password: s.config.Pass,
	}); err != nil {
		err = fmt.Errorf("can't encode request: %s", err)
		return
	}

	inputs := SendParams{
		Path:       auth,
		HttpMethod: http.MethodPost,
		Response:   response,
		Body:       body,
	}

	if _, err = sendRequest(s.config, &inputs); err != nil {
		return
	}

	response.Date = inputs.Date

	return
}

func sendRequest(config *Config, inputs *SendParams) (respBody []byte, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("softline! SendRequest: %v", err)
		}
	}()

	baseURL, err := url.Parse(config.URI)
	if err != nil {
		return respBody, fmt.Errorf("can't parse URI from config: %w", err)
	}

	// Добавляем путь из inputs.Path к базовому URL
	baseURL.Path += inputs.Path

	// Устанавливаем параметры запроса из queryParams
	query := baseURL.Query()
	for key, value := range inputs.QueryParams {
		query.Set(key, value)
	}
	baseURL.RawQuery = query.Encode()

	finalUrl := baseURL.String()

	log.Println("url: ", finalUrl)

	req, err := http.NewRequest(inputs.HttpMethod, finalUrl, inputs.Body)
	if err != nil {
		return respBody, fmt.Errorf("can't create request! Err: %s", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "application/json")

	if inputs.AuthNeed {
		req.Header.Set("AuthorizationJWT", fmt.Sprintf("Bearer %v", inputs.Token))
	}

	httpClient := http.Client{
		Transport: &http.Transport{
			IdleConnTimeout: time.Second * time.Duration(config.IdleConnTimeoutSec),
		},
		Timeout: time.Second * time.Duration(config.RequestTimeoutSec),
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return respBody, fmt.Errorf("can't do request! Err: %s", err)
	}
	defer resp.Body.Close()

	inputs.HttpCode = resp.StatusCode

	respBody, err = io.ReadAll(resp.Body)
	if err != nil {
		return respBody, fmt.Errorf("can't read response body! Err: %w", err)
	}

	if resp.StatusCode == http.StatusInternalServerError {
		return respBody, fmt.Errorf("error: %v", string(respBody))
	}

	inputs.Date = resp.Header.Get("date")

	if err = json.Unmarshal(respBody, &inputs.Response); err != nil {
		return respBody, fmt.Errorf("can't unmarshall response: '%v'. Err: %w", string(respBody), err)
	}
	return
}

func (s *Service) CreatePayment(data CreatePaymentReq, token string) (respBody []byte, response *CreatePaymentResp, err error) {
	response = new(CreatePaymentResp)

	body := new(bytes.Buffer)
	if err = json.NewEncoder(body).Encode(data); err != nil {
		err = fmt.Errorf("can't encode request: %s", err)
		return
	}

	inputs := SendParams{
		Path:       createPayment,
		HttpMethod: http.MethodPost,
		Token:      token,
		AuthNeed:   true,
		Response:   response,
		Body:       body,
	}

	if respBody, err = sendRequest(s.config, &inputs); err != nil {
		return
	}

	return
}

func (s *Service) MakePayment(data MakePaymentReq, token string) (respBody []byte, response *CreatePaymentResp, err error) {
	response = new(CreatePaymentResp)

	body := new(bytes.Buffer)
	if err = json.NewEncoder(body).Encode(data); err != nil {
		err = fmt.Errorf("can't encode request: %s", err)
		return
	}

	inputs := SendParams{
		Path:       makePayment,
		HttpMethod: http.MethodPost,
		Token:      token,
		Response:   response,
		AuthNeed:   true,
		Body:       body,
	}

	if respBody, err = sendRequest(s.config, &inputs); err != nil {
		return
	}

	return
}

func (s *Service) GenerateSignature(params Signature) string {
	message := fmt.Sprintf("%s;%s;%s;%s;%s;%s;%s", params.SecretKey, params.Event, params.OrderID,
		params.CreateDate, params.PaymentMethod, params.Currency, params.CustomerEmail)
	hash := sha512.Sum512([]byte(message))
	return hex.EncodeToString(hash[:])
}

func (s *Service) VerifySignature(signature string, params Signature) bool {
	expectedSignature := s.GenerateSignature(params)
	return signature == expectedSignature
}

func (s *Service) PostCheck(orderID string, token string) (respBody []byte, response *PaymentResp, err error) {
	response = new(PaymentResp)

	inputs := SendParams{
		Path:       fmt.Sprintf("%v%v", getPayment, orderID),
		HttpMethod: http.MethodGet,
		Token:      token,
		AuthNeed:   true,
		Response:   response,
	}

	if respBody, err = sendRequest(s.config, &inputs); err != nil {
		return
	}

	return
}

func (s *Service) Refund(request RefundReq, token string) (response *PaymentResp, err error) {
	response = new(PaymentResp)

	body := new(bytes.Buffer)
	if err = json.NewEncoder(body).Encode(request); err != nil {
		err = fmt.Errorf("can't encode request: %s", err)
		return
	}

	inputs := SendParams{
		Path:       fmt.Sprintf(refund, request.OrderID),
		HttpMethod: http.MethodPost,
		Token:      token,
		AuthNeed:   true,
		Body:       body,
		Response:   response,
	}

	if _, err = sendRequest(s.config, &inputs); err != nil && inputs.HttpCode != http.StatusOK {
		return
	}

	return response, nil
}
