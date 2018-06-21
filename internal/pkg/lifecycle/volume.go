package lifecycle

import (
	"github.com/RSE-Cambridge/data-acc/internal/pkg/registry"
	"log"
)

type VolumeLifecycleManager interface {
	DataIn() error
	Mount() error
	Unmount() error
	DataOut() error
	Delete() error // TODO allow context for timeout and cancel?
}

func NewVolumeLifecycleManager(volumeRegistry registry.VolumeRegistry, poolRegistry registry.PoolRegistry,
	volume registry.Volume) VolumeLifecycleManager {
	return &volumeLifecyceManager{volumeRegistry, poolRegistry, volume}
}

type volumeLifecyceManager struct {
	volumeRegistry registry.VolumeRegistry
	poolRegistry   registry.PoolRegistry
	volume         registry.Volume
}

func (vlm *volumeLifecyceManager) Delete() error {
	// TODO convert errors into volume related errors, somewhere?
	if vlm.volume.SizeBricks != 0 {
		err := vlm.volumeRegistry.UpdateState(vlm.volume.Name, registry.DeleteRequested)
		if err != nil {
			return err
		}
		err = vlm.volumeRegistry.WaitForState(vlm.volume.Name, registry.BricksDeleted)
		if err != nil {
			return err
		}

		// TODO should we error out here when one of these steps fail?
		err = vlm.poolRegistry.DeallocateBricks(vlm.volume.Name)
		if err != nil {
			return err
		}
		allocations, err := vlm.poolRegistry.GetAllocationsForVolume(vlm.volume.Name)
		if err != nil {
			return err
		}
		// TODO we should really wait for the brick manager to call this API
		err = vlm.poolRegistry.HardDeleteAllocations(allocations)
		if err != nil {
			return err
		}
	}
	return vlm.volumeRegistry.DeleteVolume(vlm.volume.Name)
}

func (vlm *volumeLifecyceManager) DataIn() error {
	return nil
}

func (vlm *volumeLifecyceManager) Mount() error {
	return nil
}

func (vlm *volumeLifecyceManager) Unmount() error {
	if vlm.volume.SizeBricks == 0 {
		log.Println("skipping postrun for:", vlm.volume.Name) // TODO return error type and handle outside?
		return nil
	}

	err := vlm.volumeRegistry.UpdateState(vlm.volume.Name, registry.UnmountRequested)
	if err != nil {
		return err
	}
	return vlm.volumeRegistry.WaitForState(vlm.volume.Name, registry.UnmountComplete)
}

func (vlm *volumeLifecyceManager) DataOut() error {
	return nil
}
