package mailflow

import (
	"context"
	"fmt"
	"reflect"
	"strings"
)

// Coordinator 负责编排单封邮件的处理、投递与可选删除源邮件。
type Coordinator struct {
	Processor Processor
	Store     StateStore
	Consumers map[string]Consumer
}

func (c *Coordinator) Run(ctx context.Context, envelope MailEnvelope) (Result, error) {
	if err := envelope.Validate(); err != nil {
		return Result{}, err
	}
	if c == nil || c.Processor == nil {
		return Result{}, fmt.Errorf("processor 未配置")
	}
	if c.Store == nil {
		return Result{}, fmt.Errorf("state store 未配置")
	}

	key := strings.TrimSpace(envelope.Trace.TransactionKey)
	state, found, err := c.Store.Load(ctx, key)
	if err != nil {
		return Result{}, err
	}
	if !found {
		state = TxState{
			Key:        key,
			Deliveries: make(map[string]DeliveryReceipt),
		}
	}
	if state.Deliveries == nil {
		state.Deliveries = make(map[string]DeliveryReceipt)
	}
	if state.Completed {
		return c.result(state), nil
	}

	if len(state.Plan.Targets) == 0 || !hasRequiredDeliveries(state, state.Plan) {
		processed, err := c.Processor.Process(ctx, envelope)
		if err != nil {
			return Result{}, err
		}
		if err := processed.Validate(); err != nil {
			return Result{}, err
		}
		if processed.Trace.TransactionKey != key {
			return Result{}, fmt.Errorf("processor 返回的 transaction key 与入口不一致")
		}

		if len(state.Plan.Targets) == 0 {
			state.Trace = processed.Trace
			state.Plan = processed.Plan
			if err := c.Store.Save(ctx, state); err != nil {
				return Result{}, err
			}
		} else {
			if !reflect.DeepEqual(state.Plan, processed.Plan) {
				return Result{}, fmt.Errorf("processor 返回的 execution plan 与已持久化计划不一致")
			}
		}

		for _, target := range state.Plan.Targets {
			targetKey := target.Key()
			if _, committed := state.Deliveries[targetKey]; committed {
				continue
			}

			consumer, err := c.consumer(target.Consumer)
			if err != nil {
				return Result{}, err
			}
			artifact, err := processed.Artifact(target.Artifact)
			if err != nil {
				return Result{}, err
			}

			receipt, err := consumer.Consume(ctx, ConsumeRequest{
				Trace:    processed.Trace,
				Target:   target,
				Artifact: artifact,
			})
			if err != nil {
				if target.Required {
					return Result{}, fmt.Errorf("提交目标 %s 失败: %w", targetKey, err)
				}
				continue
			}

			if strings.TrimSpace(receipt.Target) == "" {
				receipt.Target = targetKey
			}
			if strings.TrimSpace(receipt.Consumer) == "" {
				receipt.Consumer = target.Consumer
			}
			state.Deliveries[targetKey] = receipt
			if err := c.Store.Save(ctx, state); err != nil {
				return Result{}, err
			}
		}
	}

	if !hasRequiredDeliveries(state, state.Plan) {
		return c.result(state), nil
	}

	if !state.SourceDeleted && shouldDeleteSource(state, state.Plan.DeleteSource) {
		deletable, ok := envelope.Source.(DeletableSource)
		if envelope.Source == nil || !ok {
			return Result{}, fmt.Errorf("删除源邮件已启用，但来源不支持删除")
		}
		if err := deletable.Delete(ctx); err != nil {
			return Result{}, fmt.Errorf("删除源邮件失败: %w", err)
		}
		state.SourceDeleted = true
		if err := c.Store.Save(ctx, state); err != nil {
			return Result{}, err
		}
	}

	if !state.SourceAcked {
		if envelope.Source != nil {
			if err := envelope.Source.Acknowledge(ctx); err != nil {
				return Result{}, fmt.Errorf("确认来源邮件完成失败: %w", err)
			}
		}
		state.SourceAcked = true
		if err := c.Store.Save(ctx, state); err != nil {
			return Result{}, err
		}
	}

	state.Completed = true
	if err := c.Store.Save(ctx, state); err != nil {
		return Result{}, err
	}

	return c.result(state), nil
}

func (c *Coordinator) consumer(name string) (Consumer, error) {
	if c == nil {
		return nil, fmt.Errorf("coordinator 未初始化")
	}
	consumer, ok := c.Consumers[strings.TrimSpace(name)]
	if !ok || consumer == nil {
		return nil, fmt.Errorf("consumer 未配置: %s", name)
	}
	return consumer, nil
}

func (c *Coordinator) result(state TxState) Result {
	deliveries := make(map[string]DeliveryReceipt, len(state.Deliveries))
	for key, receipt := range state.Deliveries {
		deliveries[key] = receipt
	}
	return Result{
		Key:           state.Key,
		Plan:          state.Plan,
		Deliveries:    deliveries,
		SourceDeleted: state.SourceDeleted,
		SourceAcked:   state.SourceAcked,
		Completed:     state.Completed,
	}
}

func hasRequiredDeliveries(state TxState, plan ExecutionPlan) bool {
	for _, target := range plan.Targets {
		if !target.Required {
			continue
		}
		if _, ok := state.Deliveries[target.Key()]; !ok {
			return false
		}
	}
	return true
}

func shouldDeleteSource(state TxState, policy DeleteSourcePolicy) bool {
	if !policy.Enabled {
		return false
	}

	sourceStore := state.Trace.SourceStore
	eligibleConsumers := make(map[string]struct{}, len(policy.EligibleConsumers))
	for _, consumer := range policy.EligibleConsumers {
		eligibleConsumers[strings.TrimSpace(consumer)] = struct{}{}
	}

	for _, receipt := range state.Deliveries {
		if len(eligibleConsumers) > 0 {
			if _, ok := eligibleConsumers[strings.TrimSpace(receipt.Consumer)]; !ok {
				continue
			}
		}
		if !policy.RequireSameStore {
			return true
		}
		if sourceStore.Valid() && receipt.Store.Valid() && receipt.Store.Equal(sourceStore) {
			return true
		}
	}

	return false
}
