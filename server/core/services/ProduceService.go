// +build !dev

package services

import (
	"KafkaMessageQ-API/server/core/config"
	"KafkaMessageQ-API/server/core/structs/commu"
	"KafkaMessageQ-API/server/plugin"
	"KafkaMessageQ-API/server/plugin/kafkaclient"
	"KafkaMessageQ-API/server/plugin/uuid"
	"errors"
	"sync"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	jsoniter "github.com/json-iterator/go"
)

func ProduceService(pf *commu.ProduceForm, rt chan *commu.ResponseTringger) {
	var mf commu.MessageForm
	var response commu.ResponseTringger
	var wg sync.WaitGroup

	//Check the topic is exists in Kafka or not.
	kafkaResponse, err := kafkaclient.ExistsTopic(&pf.Topics, &config.AsyncConfigProduce)
	//if kafka was error return error that receive from kafka

	if err != nil && kafkaResponse == 0 {
		response.Error = err.Error()
		response.StatusCode = _serverError
		rt <- &response
		return
	} else if err != nil && kafkaResponse == -1 {
		response.Error = err.Error()
		response.StatusCode = _badRequest
		rt <- &response
		return
	} else if kafkaResponse == 0 && err == nil {
		err = errors.New("topics not found")
		response.Error = err.Error()
		response.StatusCode = _badRequest
		rt <- &response
		return
	}

	if pf.Await {
		wg.Add(1)
		errChan := make(chan error)
		go producer(pf.Topics[config.SendingMessageToTopic], true, pf, &wg, errChan)

		err = <-errChan

		if err != nil {
			response.Error = err.Error()
			response.StatusCode = _serverError
			rt <- &response
			return
		}

		configMap := config.ConfigConsume
		configMap["group.id"] = clientID.String()

		//block until receive the message
		data, err := kafkaclient.AwaitMessage(pf.Topics[config.ReceivingMessageFromTopic], &configMap, pf.TimeoutConsume)

		if err != nil {
			response.Error = err.Error()
			response.StatusCode = _serverError
			rt <- &response
			return
		}

		if err = jsoniter.Unmarshal(data, &mf.Message); err != nil {
			response.Error = err.Error()
			response.StatusCode = _serverError
			rt <- &response
			return
		}

		//response data to client
		response.Result = mf.Message
		response.StatusCode = _ok

		rt <- &response
		return
	} else {
		//not await
		wg.Add(1)
		// go produceAsync(topic, &pf, &wg)
		errChan := make(chan error)

		go producer(pf.Topics[config.SendingMessageToTopic], false, pf, &wg, errChan)
		err = <-errChan

		if err != nil {
			response.Error = err.Error()
			response.StatusCode = _serverError
			rt <- &response
			return
		}

		//return no content
		response.StatusCode = _noContent
		rt <- &response
		return
	}

	wg.Wait()
}

func producer(topic string, identify bool, pf *commu.ProduceForm, wg *sync.WaitGroup, errChan chan error) {
	templatePayload := `{"message":{{.message}},"clientID":"{{.clientID}}"}`
	defer wg.Done()

	var id uuid.UUID

	//gen ClientID
	if identify {
		if id, err = uuid.NewUUID4(); err != nil {
			errChan <- err
			return
		}
	}

	if err != nil {
		errChan <- err //buffer error  to chan error if uuid had error
		return
	}

	//check clientID if exists wil not gen new.
	//this condition use with consume need to produce message to
	//producer that using sync style
	if pf.Message["clientID"] != nil {
		idString := pf.Message["clientID"].(string)
		idObj, err := uuid.Parse(idString)
		if err != nil {
			errChan <- err
			return
		}
		clientID = &idObj
		delete(pf.Message, "clientID")
	} else if pf.Message["clientID"] == nil {
		clientID = &id
	}

	payloadMap := map[string]interface{}{
		"message":  pf.GetMessage(),
		"clientID": clientID.String(),
	}

	payload := plugin.StringFormat(templatePayload, payloadMap)

	//set config for produce message
	syncConfigMessage := &kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Value:          []byte(payload),
	}

	err = kafkaclient.Produce(&config.SyncConfigProduce, syncConfigMessage, pf.TimeoutProduce)

	if err != nil {
		errChan <- err
		return
	}
	errChan <- nil
	return
}
