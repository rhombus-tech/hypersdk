package region

import (
  "context"
  "encoding/json"
  
  "github.com/ava-labs/hypersdk/pubsub"
)

const (
  RegionEventMode byte = 0x20

  // Event types
  RegionTEEStatus    = "tee-status"
  RegionExecution    = "execution"
  RegionCrossRegion  = "cross-region"
)

type RegionEvent struct {
  RegionID    string          `json:"regionId"`
  EventType   string          `json:"eventType"`
  Timestamp   string          `json:"timestamp"`
  Data        json.RawMessage `json:"data"`
}

// Server-side registration
func (s *Server) RegionEventManager(ctx context.Context) error {
  channel := s.vm.EventManager().RegisterChannel(RegionEventMode)
  for {
      select {
      case <-ctx.Done():
          return ctx.Err()
      case e := <-channel:
          event := new(RegionEvent)
          if err := json.Unmarshal(e, event); err != nil {
              continue
          }
          // Process event
      }
  }
}

// Client-side registration
func (c *Client) ListenRegionEvents(ctx context.Context, regionID string) (<-chan *RegionEvent, error) {
  eventChan := make(chan *RegionEvent)
  
  err := c.ws.RegisterChannel(RegionEventMode, func(msg []byte) error {
      event := new(RegionEvent)
      if err := json.Unmarshal(msg, event); err != nil {
          return err
      }
      if event.RegionID == regionID {
          select {
          case eventChan <- event:
          default:
          }
      }
      return nil
  })
  
  if err != nil {
      return nil, err
  }

  return eventChan, nil
}
