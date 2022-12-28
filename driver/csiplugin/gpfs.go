/**
 * Copyright 2019 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package scale

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/IBM/ibm-spectrum-scale-csi/driver/csiplugin/connectors"
	"github.com/IBM/ibm-spectrum-scale-csi/driver/csiplugin/settings"
	"github.com/IBM/ibm-spectrum-scale-csi/driver/csiplugin/utils"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DefaultPrimaryFileset = "spectrum-scale-csi-volume-store"

	SNAP_JOB_NOT_STARTED    = 0
	SNAP_JOB_RUNNING        = 1
	SNAP_JOB_COMPLETED      = 2
	SNAP_JOB_FAILED         = 3
	VOLCOPY_JOB_FAILED      = 4
	VOLCOPY_JOB_RUNNING     = 5
	VOLCOPY_JOB_COMPLETED   = 6
	VOLCOPY_JOB_NOT_STARTED = 7
	JOB_STATUS_UNKNOWN      = 8

	STORAGECLASS_CLASSIC  = "0"
	STORAGECLASS_ADVANCED = "1"

	// Volume types
	FILE_DIRECTORYBASED_VOLUME     = "0"
	FILE_DEPENDENTFILESET_VOLUME   = "1"
	FILE_INDEPENDENTFILESET_VOLUME = "2"

	//	BLOCK_FILESET_VOLUME = 3
)

type SnapCopyJobDetails struct {
	jobStatus int
	volID     string
}

type VolCopyJobDetails struct {
	jobStatus int
	volID     string
}

// ClusterDetails stores information of the cluster.
type ClusterDetails struct {
	// id of the Spectrum Scale cluster
	id string
	// name of the Spectrum Scale cluster
	name string
	// time when the object was last updated.
	lastupdated time.Time
	// expiry duration in hours.
	expiryDuration float64
}

// ClusterName stores the name of the cluster.
type ClusterName struct {
	// name of the Spectrum Scale cluster
	name string
}

// ClusterID stores the id of the cluster.
type ClusterID struct {
	// id of the Spectrum Scale cluster
	id string
}

type ScaleDriver struct {
	name          string
	vendorVersion string
	nodeID        string

	ids *ScaleIdentityServer
	ns  *ScaleNodeServer
	cs  *ScaleControllerServer

	connmap map[string]connectors.SpectrumScaleConnector
	cmap    settings.ScaleSettingsConfigMap
	primary settings.Primary
	reqmap  map[string]int64

	snapjobstatusmap    sync.Map
	volcopyjobstatusmap sync.Map

	// clusterMap map stores the cluster name as key and cluster details as value.
	clusterMap sync.Map

	vcap  []*csi.VolumeCapability_AccessMode
	cscap []*csi.ControllerServiceCapability
	nscap []*csi.NodeServiceCapability
}

func GetScaleDriver(ctx context.Context) *ScaleDriver {
	glog.Infof("[%s] gpfs GetScaleDriver", utils.GetLoggerId(ctx))
	return &ScaleDriver{}
}

func NewIdentityServer(ctx context.Context, d *ScaleDriver) *ScaleIdentityServer {
	glog.Infof("[%s] gpfs NewIdentityServer", utils.GetLoggerId(ctx))
	return &ScaleIdentityServer{
		Driver: d,
	}
}

func NewControllerServer(ctx context.Context, d *ScaleDriver, connMap map[string]connectors.SpectrumScaleConnector, cmap settings.ScaleSettingsConfigMap, primary settings.Primary) *ScaleControllerServer {
	glog.Infof("[%s] gpfs NewControllerServer", utils.GetLoggerId(ctx))
	d.connmap = connMap
	d.cmap = cmap
	d.primary = primary
	d.reqmap = make(map[string]int64)
	return &ScaleControllerServer{
		Driver: d,
	}
}

func NewNodeServer(ctx context.Context, d *ScaleDriver) *ScaleNodeServer {
	glog.Infof("[%s] gpfs NewNodeServer", utils.GetLoggerId(ctx))
	return &ScaleNodeServer{
		Driver: d,
	}
}

func (driver *ScaleDriver) AddVolumeCapabilityAccessModes(ctx context.Context, vc []csi.VolumeCapability_AccessMode_Mode) error {
	glog.Infof("[%s] gpfs AddVolumeCapabilityAccessModes", utils.GetLoggerId(ctx))
	var vca []*csi.VolumeCapability_AccessMode
	for _, c := range vc {
		glog.V(4).Infof("[%s] Enabling volume access mode: %v", utils.GetLoggerId(ctx), c.String())
		vca = append(vca, NewVolumeCapabilityAccessMode(c))
	}
	driver.vcap = vca
	return nil
}

func (driver *ScaleDriver) AddControllerServiceCapabilities(ctx context.Context, cl []csi.ControllerServiceCapability_RPC_Type) error {
	glog.Infof("[%s] gpfs AddControllerServiceCapabilities", utils.GetLoggerId(ctx))
	var csc []*csi.ControllerServiceCapability
	for _, c := range cl {
		glog.V(4).Infof("[%s] Enabling controller service capability: %v", utils.GetLoggerId(ctx), c.String())
		csc = append(csc, NewControllerServiceCapability(c))
	}
	driver.cscap = csc
	return nil
}

func (driver *ScaleDriver) AddNodeServiceCapabilities(ctx context.Context, nl []csi.NodeServiceCapability_RPC_Type) error {
	glog.Infof("[%s] gpfs AddNodeServiceCapabilities", utils.GetLoggerId(ctx))
	var nsc []*csi.NodeServiceCapability
	for _, n := range nl {
		glog.V(4).Infof("[%s] Enabling node service capability: %v", utils.GetLoggerId(ctx), n.String())
		nsc = append(nsc, NewNodeServiceCapability(n))
	}
	driver.nscap = nsc
	return nil
}

func (driver *ScaleDriver) ValidateControllerServiceRequest(ctx context.Context, c csi.ControllerServiceCapability_RPC_Type) error {
	glog.Infof("[%s] gpfs ValidateControllerServiceRequest", utils.GetLoggerId(ctx))
	if c == csi.ControllerServiceCapability_RPC_UNKNOWN {
		return nil
	}
	for _, cap := range driver.cscap {
		if c == cap.GetRpc().Type {
			return nil
		}
	}
	return status.Error(codes.InvalidArgument, "Invalid controller service request")
}

func (driver *ScaleDriver) SetupScaleDriver(ctx context.Context, name, vendorVersion, nodeID string) error {
	glog.Infof("[%s] gpfs SetupScaleDriver. name: %s, version: %v, nodeID: %s", utils.GetLoggerId(ctx), name, vendorVersion, nodeID)
	if name == "" {
		return fmt.Errorf("Driver name missing")
	}

	scmap, cmap, primary, err := driver.PluginInitialize(ctx)
	if err != nil {
		glog.Errorf("[%s] Error in plugin initialization: %s", utils.GetLoggerId(ctx), err)
		return err
	}

	driver.name = name
	driver.vendorVersion = vendorVersion
	driver.nodeID = nodeID

	// Adding Capabilities
	vcam := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}
	_ = driver.AddVolumeCapabilityAccessModes(ctx, vcam)

	csc := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
		csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
	}
	_ = driver.AddControllerServiceCapabilities(ctx, csc)

	ns := []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
	}
	_ = driver.AddNodeServiceCapabilities(ctx, ns)

	driver.ids = NewIdentityServer(ctx, driver)
	driver.ns = NewNodeServer(ctx, driver)
	driver.cs = NewControllerServer(ctx, driver, scmap, cmap, primary)
	return nil
}

func (driver *ScaleDriver) PluginInitialize(ctx context.Context) (map[string]connectors.SpectrumScaleConnector, settings.ScaleSettingsConfigMap, settings.Primary, error) { //nolint:funlen
	glog.Infof("[%s] gpfs PluginInitialize", utils.GetLoggerId(ctx))
	scaleConfig := settings.LoadScaleConfigSettings(ctx)

	isValid, err := driver.ValidateScaleConfigParameters(ctx, scaleConfig)
	if !isValid {
		glog.Errorf("[%s] Parameter validation failure", utils.GetLoggerId(ctx))
		return nil, settings.ScaleSettingsConfigMap{}, settings.Primary{}, err
	}

	scaleConnMap := make(map[string]connectors.SpectrumScaleConnector)
	primaryInfo := settings.Primary{}
	remoteFilesystemName := ""

	for i := 0; i < len(scaleConfig.Clusters); i++ {
		cluster := scaleConfig.Clusters[i]

		sc, err := connectors.GetSpectrumScaleConnector(ctx, cluster)
		if err != nil {
			glog.Errorf("[%s] Unable to initialize Spectrum Scale connector for cluster %s", utils.GetLoggerId(ctx), cluster.ID)
			return nil, scaleConfig, primaryInfo, err
		}

		// validate cluster ID
		clusterId, err := sc.GetClusterId(ctx)
		if err != nil {
			glog.Errorf("[%s] Error getting cluster ID: %v", utils.GetLoggerId(ctx), err)
			return nil, scaleConfig, primaryInfo, err
		}
		if cluster.ID != clusterId {
			glog.Errorf("[%s] Cluster ID %s from scale config doesnt match the ID from cluster %s.", utils.GetLoggerId(ctx), cluster.ID, clusterId)
			return nil, scaleConfig, primaryInfo, fmt.Errorf("Cluster ID doesnt match the cluster")
		}

		scaleConnMap[clusterId] = sc

		if cluster.Primary != (settings.Primary{}) {
			scaleConnMap["primary"] = sc

			// check if primary filesystem exists
			fsMount, err := sc.GetFilesystemMountDetails(ctx, cluster.Primary.GetPrimaryFs())
			if err != nil {
				glog.Errorf("[%s] Error in getting filesystem details for %s", utils.GetLoggerId(ctx), cluster.Primary.GetPrimaryFs())
				return nil, scaleConfig, cluster.Primary, err
			}

			// check if filesystem is mounted on GUI node
			isFsMounted, err := sc.IsFilesystemMountedOnGUINode(ctx, cluster.Primary.GetPrimaryFs())
			if err != nil {
				glog.Errorf("[%s] Error in getting filesystem mount details for %s on Primary cluster", utils.GetLoggerId(ctx), cluster.Primary.GetPrimaryFs())
				return nil, scaleConfig, cluster.Primary, err
			}
			if !isFsMounted {
				glog.Errorf("[%s] Primary filesystem %s is not mounted on GUI node of Primary cluster", utils.GetLoggerId(ctx), cluster.Primary.GetPrimaryFs())
				return nil, scaleConfig, cluster.Primary, fmt.Errorf("Primary filesystem %s not mounted on GUI node Primary cluster", cluster.Primary.GetPrimaryFs())
			}

			// In case primary fset value is not specified in configuation then use default
			if scaleConfig.Clusters[i].Primary.PrimaryFset == "" {
				scaleConfig.Clusters[i].Primary.PrimaryFset = DefaultPrimaryFileset
				glog.V(4).Infof("[%s] primaryFset is not specified in configuration using default %s", utils.GetLoggerId(ctx), DefaultPrimaryFileset)
			}
			scaleConfig.Clusters[i].Primary.PrimaryFSMount = fsMount.MountPoint
			scaleConfig.Clusters[i].Primary.PrimaryCid = clusterId

			primaryInfo = scaleConfig.Clusters[i].Primary

			// RemoteFS name from Local Filesystem details
			remoteDeviceName := strings.Split(fsMount.RemoteDeviceName, ":")
			remoteFilesystemName = remoteDeviceName[len(remoteDeviceName)-1]
		}
		// //check if multiple GUIs are passed
		// if len(cluster.RestAPI) > 1 {
		// 	err := driver.cs.checkGuiHASupport(sc)
		// 	if err != nil {
		// 		return nil, scaleConfig, cluster.Primary, err
		// 	}
		// }
	}

	fs := primaryInfo.GetPrimaryFs()
	sconn := scaleConnMap["primary"]
	fsmount := primaryInfo.PrimaryFSMount
	if primaryInfo.RemoteCluster != "" {
		sconn = scaleConnMap[primaryInfo.RemoteCluster]
		if remoteFilesystemName == "" {
			return scaleConnMap, scaleConfig, primaryInfo, fmt.Errorf("Failed to get the name of remote Filesystem")
		}
		fs = remoteFilesystemName
		// check if primary filesystem exists on remote cluster and mounted on atleast one node
		fsMount, err := sconn.GetFilesystemMountDetails(ctx, fs)
		if err != nil {
			glog.Errorf("[%s] Error in getting filesystem details for %s from cluster %s", utils.GetLoggerId(ctx), fs, primaryInfo.RemoteCluster)
			return scaleConnMap, scaleConfig, primaryInfo, err
		}

		glog.Infof("[%s] remote fsMount = %v", utils.GetLoggerId(ctx), fsMount)
		fsmount = fsMount.MountPoint

		// check if filesystem is mounted on GUI node
		isPfsMounted, err := sconn.IsFilesystemMountedOnGUINode(ctx, fs)
		if err != nil {
			glog.Errorf("[%s] Error in getting filesystem mount details for %s from cluster %s", utils.GetLoggerId(ctx), fs, primaryInfo.RemoteCluster)
			return scaleConnMap, scaleConfig, primaryInfo, err
		}

		if !isPfsMounted {
			glog.Errorf("[%s] Filesystem %s is not mounted on GUI node of cluster %s", utils.GetLoggerId(ctx), fs, primaryInfo.RemoteCluster)
			return scaleConnMap, scaleConfig, primaryInfo, fmt.Errorf("Filesystem %s is not mounted on GUI node of cluster %s", fs, primaryInfo.RemoteCluster)
		}
	}

	fsetlinkpath, err := driver.CreatePrimaryFileset(ctx, sconn, fs, fsmount, primaryInfo.PrimaryFset, primaryInfo.GetInodeLimit())
	if err != nil {
		glog.Errorf("[%s] Error in creating primary fileset", utils.GetLoggerId(ctx))
		return scaleConnMap, scaleConfig, primaryInfo, err
	}

	// In case primary FS is remotely mounted, run fileset refresh task on primary cluster
	if primaryInfo.RemoteCluster != "" {
		_, err := scaleConnMap["primary"].ListFileset(ctx, primaryInfo.GetPrimaryFs(), primaryInfo.PrimaryFset)
		if err != nil {
			glog.Infof("[%s] Primary fileset %v not visible on primary cluster. Running fileset refresh task", utils.GetLoggerId(ctx), primaryInfo.PrimaryFset)
			err = scaleConnMap["primary"].FilesetRefreshTask(ctx)
			if err != nil {
				glog.Errorf("[%s] Error in fileset refresh task", utils.GetLoggerId(ctx))
				return scaleConnMap, scaleConfig, primaryInfo, err
			}
		}

		// retry listing fileset again after some time after refresh
		time.Sleep(8 * time.Second)
		_, err = scaleConnMap["primary"].ListFileset(ctx, primaryInfo.GetPrimaryFs(), primaryInfo.PrimaryFset)
		if err != nil {
			glog.Errorf("[%s] Primary fileset %v not visible on primary cluster even after running fileset refresh task", utils.GetLoggerId(ctx), primaryInfo.PrimaryFset)
			return scaleConnMap, scaleConfig, primaryInfo, err
		}
	}

	if fsmount != primaryInfo.PrimaryFSMount {
		fsetlinkpath = strings.Replace(fsetlinkpath, fsmount, primaryInfo.PrimaryFSMount, 1)
	}

	// Create directory where volume symlinks will reside
	symlinkPath, relativePath, err := driver.CreateSymlinkPath(ctx, scaleConnMap["primary"], primaryInfo.GetPrimaryFs(), primaryInfo.PrimaryFSMount, fsetlinkpath)
	if err != nil {
		glog.Errorf("[%s] Error in creating volumes directory", utils.GetLoggerId(ctx))
		return scaleConnMap, scaleConfig, primaryInfo, err
	}
	primaryInfo.SymlinkAbsolutePath = symlinkPath
	primaryInfo.SymlinkRelativePath = relativePath
	primaryInfo.PrimaryFsetLink = fsetlinkpath

	glog.Infof("[%s] IBM Spectrum Scale: Plugin initialized", utils.GetLoggerId(ctx))
	return scaleConnMap, scaleConfig, primaryInfo, nil
}

func (driver *ScaleDriver) CreatePrimaryFileset(ctx context.Context, sc connectors.SpectrumScaleConnector, primaryFS string, fsmount string, filesetName string, inodeLimit string) (string, error) {
	glog.Infof("[%s] gpfs CreatePrimaryFileset. primaryFS: %s, mountpoint: %s, filesetName: %s", utils.GetLoggerId(ctx), primaryFS, fsmount, filesetName)

	// create primary fileset if not already created
	fsetResponse, err := sc.ListFileset(ctx, primaryFS, filesetName)
	linkpath := fsetResponse.Config.Path
	newlinkpath := path.Join(fsmount, filesetName)

	if err != nil {
		glog.Errorf("[%s] Primary fileset %s not found. Creating it.", utils.GetLoggerId(ctx), filesetName)
		opts := make(map[string]interface{})
		if inodeLimit != "" {
			opts[connectors.UserSpecifiedInodeLimit] = inodeLimit
		}

		err = sc.CreateFileset(ctx, primaryFS, filesetName, opts)
		if err != nil {
			glog.Errorf("[%s] Unable to create primary fileset %s", utils.GetLoggerId(ctx), filesetName)
			return "", err
		}
		linkpath = newlinkpath
	} else if linkpath == "" || linkpath == "--" {
		glog.Infof("[%s] Primary fileset %s not linked. Linking it.", utils.GetLoggerId(ctx), filesetName)
		err = sc.LinkFileset(ctx, primaryFS, filesetName, newlinkpath)
		if err != nil {
			glog.Errorf("[%s] Unable to link primary fileset %s", utils.GetLoggerId(ctx), filesetName)
			return "", err
		} else {
			glog.Infof("[%s] Linked primary fileset %s. Linkpath: %s", utils.GetLoggerId(ctx), newlinkpath, filesetName)
		}
		linkpath = newlinkpath
	} else {
		glog.Infof("[%s] Primary fileset %s exists and linked at %s", utils.GetLoggerId(ctx), filesetName, linkpath)
	}

	return linkpath, nil
}

func (driver *ScaleDriver) CreateSymlinkPath(ctx context.Context, sc connectors.SpectrumScaleConnector, fs string, fsmount string, fsetlinkpath string) (string, string, error) {
	glog.V(4).Infof("[%s] gpfs CreateSymlinkPath. filesystem: %s, mountpoint: %s, filesetlinkpath: %s", utils.GetLoggerId(ctx), fs, fsmount, fsetlinkpath)

	dirpath := strings.Replace(fsetlinkpath, fsmount, "", 1)
	dirpath = strings.Trim(dirpath, "!/")
	fsetlinkpath = strings.TrimSuffix(fsetlinkpath, "/")

	dirpath = fmt.Sprintf("%s/.volumes", dirpath)
	symlinkpath := fmt.Sprintf("%s/.volumes", fsetlinkpath)

	err := sc.MakeDirectory(ctx, fs, dirpath, "0", "0")
	if err != nil {
		glog.Errorf("[%s] Make directory failed on filesystem %s, path = %s", utils.GetLoggerId(ctx), fs, dirpath)
		return symlinkpath, dirpath, err
	}

	return symlinkpath, dirpath, nil
}

// ValidateScaleConfigParameters : Validating the Configuration provided for Spectrum Scale CSI Driver
func (driver *ScaleDriver) ValidateScaleConfigParameters(ctx context.Context, scaleConfig settings.ScaleSettingsConfigMap) (bool, error) {
	glog.V(4).Infof("[%s] gpfs ValidateScaleConfigParameters.", utils.GetLoggerId(ctx))
	if len(scaleConfig.Clusters) == 0 {
		return false, fmt.Errorf("Missing cluster information in Spectrum Scale configuration")
	}

	primaryClusterFound := false
	rClusterForPrimaryFS := ""
	var cl = make([]string, len(scaleConfig.Clusters))
	issueFound := false

	for i := 0; i < len(scaleConfig.Clusters); i++ {
		cluster := scaleConfig.Clusters[i]

		if cluster.ID == "" {
			issueFound = true
			glog.Errorf("[%s] Mandatory parameter 'id' is not specified", utils.GetLoggerId(ctx))
		}
		if len(cluster.RestAPI) == 0 {
			issueFound = true
			glog.Errorf("[%s] Mandatory section 'restApi' is not specified for cluster %v", utils.GetLoggerId(ctx), cluster.ID)
		}
		if len(cluster.RestAPI) != 0 && cluster.RestAPI[0].GuiHost == "" {
			issueFound = true
			glog.Errorf("[%s] Mandatory parameter 'guiHost' is not specified for cluster %v", utils.GetLoggerId(ctx), cluster.ID)
		}

		if cluster.Primary != (settings.Primary{}) {
			if primaryClusterFound {
				issueFound = true
				glog.Errorf("[%s] More than one primary clusters specified", utils.GetLoggerId(ctx))
			}

			primaryClusterFound = true

			if cluster.Primary.GetPrimaryFs() == "" {
				issueFound = true
				glog.Errorf("[%s] Mandatory parameter 'primaryFs' is not specified for primary cluster %v", utils.GetLoggerId(ctx), cluster.ID)
			}

			rClusterForPrimaryFS = cluster.Primary.RemoteCluster
		} else {
			cl[i] = cluster.ID
		}

		if cluster.Secrets == "" {
			issueFound = true
			glog.Errorf("[%s] Mandatory parameter 'secrets' is not specified for cluster %v", utils.GetLoggerId(ctx), cluster.ID)
		}

		if cluster.SecureSslMode && cluster.CacertValue == nil {
			issueFound = true
			glog.Errorf("[%s] CA certificate not specified in secure SSL mode for cluster %v", utils.GetLoggerId(ctx), cluster.ID)
		}
	}

	if !primaryClusterFound {
		issueFound = true
		glog.Errorf("[%s] No primary clusters specified", utils.GetLoggerId(ctx))
	}

	if rClusterForPrimaryFS != "" && !utils.StringInSlice(rClusterForPrimaryFS, cl) {
		issueFound = true
		glog.Errorf("[%s] Remote cluster specified for primary filesystem: %s, but no definition found for it in config", utils.GetLoggerId(ctx), rClusterForPrimaryFS)
	}

	if issueFound {
		return false, fmt.Errorf("one or more issue found in Spectrum scale csi driver configuration, check Spectrum Scale csi driver logs")
	}

	return true, nil
}

func (driver *ScaleDriver) Run(ctx context.Context, endpoint string) {
	glog.Infof("[%s] Driver: %v version: %v", utils.GetLoggerId(ctx), driver.name, driver.vendorVersion)
	s := NewNonBlockingGRPCServer()
	s.Start(endpoint, driver.ids, driver.cs, driver.ns)
	s.Wait()
}
