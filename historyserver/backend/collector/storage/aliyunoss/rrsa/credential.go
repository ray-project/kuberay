package rrsa

import (
	"log"

	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/aliyun/credentials-go/credentials"
)

func newCredential() (credentials.Credential, error) {
	// https://www.alibabacloud.com/help/doc-detail/378661.html
	cred, err := credentials.NewCredential(nil)
	return cred, err
}

type Credentials struct {
	AccessKeyId     string
	AccessKeySecret string
	SecurityToken   string
}

func NewProvider() (credentials.Credential, error) {
	return newCredential()
}

func NewOssProvider() (*OssCredentialsProvider, error) {
	cred, err := newCredential()
	if err != nil {
		return nil, err
	}
	return &OssCredentialsProvider{cred: cred}, nil
}

type OssCredentialsProvider struct {
	cred credentials.Credential
}

func (c *Credentials) GetAccessKeyID() string {
	return c.AccessKeyId
}

func (c *Credentials) GetAccessKeySecret() string {
	return c.AccessKeySecret
}

func (c *Credentials) GetSecurityToken() string {
	return c.SecurityToken
}

func (p *OssCredentialsProvider) GetCredentials() oss.Credentials {
	id, err := p.cred.GetAccessKeyId()
	if err != nil {
		log.Printf("get access key id failed: %+v", err)
		return &Credentials{}
	}
	secret, err := p.cred.GetAccessKeySecret()
	if err != nil {
		log.Printf("get access key secret failed: %+v", err)
		return &Credentials{}
	}
	token, err := p.cred.GetSecurityToken()
	if err != nil {
		log.Printf("get access security token failed: %+v", err)
		return &Credentials{}
	}

	return &Credentials{
		AccessKeyId:     tea.StringValue(id),
		AccessKeySecret: tea.StringValue(secret),
		SecurityToken:   tea.StringValue(token),
	}
}
