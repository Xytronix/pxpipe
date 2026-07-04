package main

import (
	"encoding/json"
	"fmt"

	"github.com/maximhq/bifrost/core/schemas"
	"github.com/maximhq/bifrost/plugins/pxpipe"
)

var instance *pxpipe.PxpipePlugin

func Init(config any) error {
	var cfg pxpipe.Config
	if config != nil {
		raw, err := json.Marshal(config)
		if err != nil {
			return fmt.Errorf("pxpipe: marshal config: %w", err)
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("pxpipe: decode config: %w", err)
		}
	}
	p, err := pxpipe.Init(cfg, nil)
	if err != nil {
		return err
	}
	instance = p
	return nil
}

func GetName() string {
	if instance != nil {
		return instance.GetName()
	}
	return pxpipe.PluginName
}

func PreRequestHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) error {
	if instance == nil {
		return nil
	}
	return instance.PreRequestHook(ctx, req)
}

func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	if instance == nil {
		return req, nil, nil
	}
	return instance.PreLLMHook(ctx, req)
}

func PostLLMHook(ctx *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	if instance == nil {
		return resp, bifrostErr, nil
	}
	return instance.PostLLMHook(ctx, resp, bifrostErr)
}

func Cleanup() error {
	if instance != nil {
		return instance.Cleanup()
	}
	return nil
}

func main() {}
