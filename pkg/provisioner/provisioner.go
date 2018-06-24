package provisioner

import (
	"fmt"

	"github.com/kubernetes-incubator/external-storage/lib/controller"
	"github.com/mitchellh/mapstructure"

	"github.com/Sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	zfs "github.com/simt2/go-zfs"
)

const (
	annCreatedBy   = "kubernetes.io/createdby"
	annDatasetPath = "gentics.com/kubernetes-zfs-provisioner/datasetpath"
	createdBy      = "zfs-provisioner"
)

// ZFSProvisionerParameters contains attributes related to ZFS, exporting of
// created volumes and metrics. The "parameters" field in a storageClass
// backed by this provisioner represents ZFSProvisionerParameters.
type ZFSProvisionerParameters struct {
	ParentDataset string        `mapstructure:"parentDataset"`
	Prometheus    bool          `mapstructure:"prometheus"`
	NFS           NFSParameters `mapstructure:"nfs"`
}

// NFSParameters contains attributes related to exporting volumes via NFS.
type NFSParameters struct {
	AdditonalShareOptions string `mapstructure:"additionalShareOptions"`
	Enabled               bool   `mapstructure:"enabled"`
	ServerHostname        string `mapstructure:"serverHostname"`
	ShareSubnet           string `mapstructure:"shareSubnet"`
}

// ZFSProvisioner implements the Provisioner interface to create and export ZFS
// volumes. It implements
// github.com/kubernetes-incubator/external-storage/lib/controller.Provisioner
type ZFSProvisioner struct {
	logger *logrus.Entry
}

// NewZFSProvisioner returns a ZFSProvisioner based on a given storageClass.
func NewZFSProvisioner(logger *logrus.Entry, storageClass *storagev1.StorageClass) (*ZFSProvisioner, error) {
	// Create a new logger if none is given and/or add the StorageClass name to
	// its fields.
	if logger == nil {
		logger = logrus.NewEntry(logrus.New())
	}
	logger = logger.WithField("storageclass", storageClass.Name)

	provisioner := ZFSProvisioner{
		logger,
	}
	return &provisioner, nil
}

// Delete destroys a ZFS dataset representing a given PersistentVolume.
func (p ZFSProvisioner) Delete(volume *corev1.PersistentVolume) error {
	logger := p.logger.WithFields(logrus.Fields{
		"pv":        volume.Name,
		"namespace": volume.Namespace,
		"dataset":   volume.Annotations[annDatasetPath],
	})

	// Retrieve volume for deletion
	datasetPath := volume.Annotations[annDatasetPath]
	dataset, err := zfs.GetDataset(datasetPath)
	if err != nil {
		logger.WithField("error", err.Error()).Error("Retrieving dataset for destruction failed")

		return fmt.Errorf("Retrieving dataset for destruction failed: %s", err.Error())
	}

	// Attempt to destroy dataset
	if err := dataset.Destroy(zfs.DestroyRecursive); err != nil {
		logger.WithField("error", err.Error()).Error("Destroying dataset failed")

		return fmt.Errorf("Destroying dataset failed: %s", err.Error())
	}

	logger.Info("Destroyed PersistentVolume")
	return nil
}

// Provision creates a ZFS dataset representing a PersistentVolume from given
// VolumeOptions.
func (p ZFSProvisioner) Provision(options controller.VolumeOptions) (*corev1.PersistentVolume, error) {
	logger := p.logger.WithFields(logrus.Fields{
		"pvc":          options.PVC.Name,
		"namespace":    options.PVC.Namespace,
		"storageclass": options.PVC.Spec.StorageClassName,
	})

	// Prepare PersistentVolume to return later
	annotations := make(map[string]string)
	annotations[annCreatedBy] = createdBy
	pv := corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:        options.PVName,
			Labels:      map[string]string{},
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: options.PersistentVolumeReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: corev1.ResourceList{
				corev1.ResourceName(corev1.ResourceStorage): options.PVC.Spec.Resources.Requests[corev1.ResourceName(corev1.ResourceStorage)],
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{},
		},
	}

	// Parse parameters and convert them to ZFSProvisionerParameters
	var parameters ZFSProvisionerParameters
	if err := mapstructure.Decode(options.Parameters, &parameters); err != nil {
		logger.WithField("error", err).Error("Parsing StorageClass parameters failed")

		return nil, fmt.Errorf("Parsing StorageClass parameters failed: %s", err.Error())
	}

	// Build new dataset name and properties
	// TODO: Sanitize PVName
	datasetPath := fmt.Sprintf("%s/%s", parameters.ParentDataset, options.PVName)
	// Annotate dataset path for deletion
	annotations[annDatasetPath] = datasetPath
	datasetProperties := make(map[string]string)

	// Convert storage limits and requests
	resources := options.PVC.Spec.Resources
	// A storage limit is represented by ZFS refquota
	limitQuantity := resources.Limits["storage"]
	limitQuantityP := &limitQuantity
	limitBytes, ok := limitQuantityP.AsInt64()
	if !ok {
		logger.Error("Could not convert storage limit to bytes")

		return nil, fmt.Errorf("Could not convert storage limit to bytes")
	}
	datasetProperties["refquota"] = string(limitBytes)
	// A storage request is represented by ZFS refreservation
	requestQuantity := resources.Requests["storage"]
	requestQuantityP := &requestQuantity
	requestBytes, ok := requestQuantityP.AsInt64()
	if !ok {
		logger.Error("Could not convert storage request to bytes")

		return nil, fmt.Errorf("Could not convert storage request to bytes")
	}
	datasetProperties["refreservation"] = string(requestBytes)

	// Set optional NFS share options
	nfs := parameters.NFS
	if nfs.Enabled {
		datasetProperties["sharenfs"] = fmt.Sprintf("rw=@%s%s", nfs.ShareSubnet, nfs.AdditonalShareOptions)

		pv.Spec.PersistentVolumeSource.NFS = &corev1.NFSVolumeSource{
			Server:   nfs.ServerHostname,
			Path:     datasetPath,
			ReadOnly: false,
		}
	}

	// Create dataset
	dataset, err := zfs.CreateFilesystem(datasetPath, datasetProperties)
	if err != nil {
		logger.WithField("error", err).Error("Creating ZFS dataset failed")

		return nil, fmt.Errorf("Creating ZFS dataset failed: %s", err.Error())
	}

	logger.WithField("dataset", dataset.Name).Info("Created PersistentVolume")
	return &pv, nil
}
