package workflow_impl

import (
	"context"
	"fmt"
	"github.com/RSE-Cambridge/data-acc/internal/pkg/v2/datamodel"
	"github.com/RSE-Cambridge/data-acc/internal/pkg/v2/facade"
	"github.com/RSE-Cambridge/data-acc/internal/pkg/v2/registry"
	"log"
	"math"
	"math/rand"
	"time"
)

func NewSessionFacade() facade.Session {
	return sessionFacade{}
}

type sessionFacade struct {
	session     registry.SessionRegistry
	actions     registry.SessionActions
	allocations registry.AllocationRegistry
	pool        registry.PoolRegistry
}

func (s sessionFacade) CreateSession(session datamodel.Session) error {
	err := s.validateSession(session)
	if err != nil {
		return err
	}

	// Get session lock
	sessionMutex, err := s.session.GetSessionMutex(session.Name)
	if err != nil {
		return fmt.Errorf("unable to get session mutex: %s due to: %s", session.Name, err)
	}
	err = sessionMutex.Lock(context.TODO())
	if err != nil {
		return fmt.Errorf("unable to lock session mutex: %s due to: %s", session.Name, err)
	}

	// Allocate bricks, and choose brick host server
	session, err = s.doSessionAllocation(session)
	// if no bricks allocated, no need to call CreateSessionVolume
	if err != nil || session.ActualSizeBytes == 0 {
		sessionMutex.Unlock(context.TODO())
		return err
	}

	// Create filesystem on the brick host server
	// TODO: add timeout
	eventChan, createErr := s.actions.CreateSessionVolume(context.TODO(), session.Name)

	// Drop mutex so the server can take it
	err = sessionMutex.Unlock(context.TODO())
	if err != nil {
		// TODO: cancel above action?
		return err
	}

	// Always drop the mutex, but check error before checking the channel
	if createErr != nil {
		// TODO: session should go into Error State? Maybe its up the above call in this case?
		return createErr
	}

	// Wait for the server to create the filesystem
	sessionAction := <-eventChan
	return sessionAction.Error
}

func (s sessionFacade) validateSession(session datamodel.Session) error {
	_, err := s.pool.GetPool(session.VolumeRequest.PoolName)
	if err != nil {
		return fmt.Errorf("invalid session, unable to find pool %s", session.VolumeRequest.PoolName)
	}
	// TODO: check for multi-job restrictions, etc?
	return nil
}

func (s sessionFacade) doSessionAllocation(session datamodel.Session) (datamodel.Session, error) {
	if session.VolumeRequest.TotalCapacityBytes > 0 {
		allocationMutex, err := s.allocations.GetAllocationMutex()
		if err != nil {
			return session, err
		}

		err = allocationMutex.Lock(context.TODO())
		if err != nil {
			return session, err
		}
		defer allocationMutex.Unlock(context.TODO())

		// Write allocations before creating the session
		actualSizeBytes, chosenBricks, err := s.getBricks(session.VolumeRequest.PoolName, session.VolumeRequest.TotalCapacityBytes)
		if err != nil {
			return session, fmt.Errorf("can't allocate for session: %s due to %s", session.Name, err)
		}

		allocations, err := s.allocations.CreateAllocations(session.Name, chosenBricks)
		if err != nil {
			return session, err
		}

		session.ActualSizeBytes = actualSizeBytes
		session.Allocations = allocations
		session.PrimaryBrickHost = allocations[0].Brick.BrickHostName
	}

	// Store initial version of session
	// returned session will have updated revision info
	session, err := s.session.CreateSession(session)
	if err != nil {
		if session.Allocations != nil {
			deleteErr := s.allocations.DeleteAllocations(session.Allocations)
			if deleteErr != nil {
				log.Println("Failed to clean up allocations due to:", deleteErr)
			}
		}
	}
	return session, err
}

func (s sessionFacade) getBricks(poolName datamodel.PoolName, bytes int) (int, []datamodel.Brick, error) {
	pool, err := s.allocations.GetPoolInfo(poolName)
	if err != nil {
		return 0, nil, err
	}

	bricksRequired := int(math.Ceil(float64(bytes) / float64(pool.Pool.GranularityBytes)))
	actualSize := bricksRequired * int(pool.Pool.GranularityBytes)

	bricks := pickBricks(bricksRequired, pool)
	if len(bricks) != bricksRequired {
		return 0, nil, fmt.Errorf(
			"unable to get number of requested bricks (%d) for given pool (%s)",
			bricksRequired, pool.Pool.Name)
	}
	return actualSize, bricks, nil
}

func pickBricks(bricksRequired int, poolInfo datamodel.PoolInfo) []datamodel.Brick {
	// pick some of the available bricks
	s := rand.NewSource(time.Now().Unix())
	r := rand.New(s) // initialize local pseudorandom generator

	var chosenBricks []datamodel.Brick
	randomWalk := r.Perm(len(poolInfo.AvailableBricks))
	for _, i := range randomWalk {
		candidateBrick := poolInfo.AvailableBricks[i]

		// TODO: should not the random walk mean this isn't needed!
		goodCandidate := true
		for _, brick := range chosenBricks {
			if brick == candidateBrick {
				goodCandidate = false
				break
			}
		}
		if goodCandidate {
			chosenBricks = append(chosenBricks, candidateBrick)
		}
		if len(chosenBricks) >= bricksRequired {
			break
		}
	}
	return chosenBricks
}

func (s sessionFacade) DeleteSession(sessionName datamodel.SessionName, hurry bool) error {
	// Get session lock
	sessionMutex, err := s.session.GetSessionMutex(sessionName)
	if err != nil {
		return fmt.Errorf("unable to get session mutex: %s due to: %s", sessionName, err)
	}
	err = sessionMutex.Lock(context.TODO())
	if err != nil {
		return fmt.Errorf("unable to lock session mutex: %s due to: %s", sessionName, err)
	}

	session, err := s.session.GetSession(sessionName)
	if err != nil {
		log.Println("Unable to find session:", sessionName)
		sessionMutex.Unlock(context.TODO())
		return nil
	}

	// Record we want this deleted, in case host is not alive
	// can be deleted when it is next stated
	session.Status.DeleteRequested = true
	session.Status.DeleteSkipCopyDataOut = hurry
	session, err = s.session.UpdateSession(session)
	if err != nil {
		sessionMutex.Unlock(context.TODO())
		return err
	}
	// TODO: send the hurry, i.e. request data copy out first

	if session.PrimaryBrickHost == "" {
		// TODO: session has no primary brick host
		//  so need to do local umount

	}

	// This will error out if the host is not currently up
	sessionAction, err := s.actions.SendSessionAction(context.TODO(), datamodel.SessionActionType("delete"), session)
	if err != nil {
		return err
	}

	// Drop mutex to allow server to lock the session
	err = sessionMutex.Unlock(context.TODO())
	if err != nil {
		// TODO: cancel above waiting around?
		return err
	}

	// wait for server to complete, or timeout
	result := <-sessionAction
	return result.Error
}

func (s sessionFacade) CopyDataIn(sessionName datamodel.SessionName) error {
	panic("implement me")
}

func (s sessionFacade) Mount(sessionName datamodel.SessionName, computeNodes []string, loginNodes []string) error {
	panic("implement me")
}

func (s sessionFacade) Unmount(sessionName datamodel.SessionName) error {
	panic("implement me")
}

func (s sessionFacade) CopyDataOut(sessionName datamodel.SessionName) error {
	panic("implement me")
}

func (s sessionFacade) GetPools() ([]datamodel.PoolInfo, error) {
	panic("implement me")
}

func (s sessionFacade) GetSession(sessionName datamodel.SessionName) (datamodel.Session, error) {
	panic("implement me")
}

func (s sessionFacade) GetAllSessions() ([]datamodel.Session, error) {
	panic("implement me")
}
