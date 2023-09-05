# Snapshots as read-only volumes

CSI spec do not have concept of mounting a snapshot. The only way is to create
new volume by copying content of snapshot and then mount that volume for
workloads.

IBM Storage Scale exposes snapshots as special, read-only directories
located in `'fileset'/.snapshots`. IBM Storage Scale CSI can already provision
writable volumes with snapshots as their data source, where snapshot contents
are cloned to the newly created volume. However, cloning a snapshot to
volume is a very expensive operation as the data needs to be fully copied.
When the need is to only read snapshot contents, snapshot cloning is extremely
inefficient and wasteful.

This proposal describes a way for IBM Storage Scale CSI to expose snapshots as
shallow, read-only volumes, without needing to clone the underlying snapshot
data.

## Design

Key points:

* Volume source is a snapshot, volume access mode is `*_READER_ONLY`.
* No actual new volume are created in Storage Scale.
* Volume Handle must contain all the required details to find snapshot in a
 fileset for various operations
* snapshot and Independent fileset dependancy must be handled properly `#BookKeeping` `#ReferenceCount`

## StorageClass

Ideally user will create with with AccessMode=ReadOnlyMany with same storageClass as of source pvc and if there is different then it might not cause issue but to keep it simple and alined we will mandate following

1. Version must be same
2. VolumeBackend must be same
3. FilesetType must be same in case of version1

## CreateVolume

A read-only volume with snapshot source would be created under these conditions:

1. `CreateVolumeRequest.volume_content_source` is a snapshot,
2. `CreateVolumeRequest.volume_capabilities[*].access_mode` is any of read-only
   volume access modes.

#### Create Volume Flow

1. Check if independent fileset and snapshot exist. Details of Independent fileset/snapshot will be in createVolume request under snapshotHandle. 

snapshot here is the actual snapshot on scale and not the reference snapshot defined for version 2 snapshots

for example -

a. Version 1 snapshot handle will look like following. here snapshot name is `snapshot-b4f6236f-01e4-4c67-9bf5-39ec0969c9f9`


   ```0;2;4033149527292681937;5D3D0B0A:64509FB6;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379;snapshot-b4f6236f-01e4-4c67-9bf5-39ec0969c9f9;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379-data```


b. Version 2 snapshot handle will look like following. here snapshot name is `snapshot-b515a1c2-9fe6-47df-9a86-10a20b7965c6`


```1;1;4033149527292681937;5D3D0B0A:64509FB6;4c6db64a-32ea-4c7a-9768-a387539af470-default;pvc-5cf0ed9b-6b58-4313-8442-8df57bed6229;snapshot-b515a1c2-9fe6-47df-9a86-10a20b7965c6;snapshot-0f0d7c6b-d1dc-4214-9144-c3dba47720ed```

2. Create directory under "independent-fileset-name" with the name 'snapshot name' if not present `#BookKeeping` `#ReferenceCount`
   **Note: We must have mutex to create and delete directory**
   
3. Create another directory under 'independent-fileset-name'/'snapshot-name'/'volume-name' `#BookKeeping` `#ReferenceCount`
 
4. Return volume handle with path `/ibm/fs1/'independent-fileset-name'/.snapshots/'snapshot-name'/'src-volume-path'`
From above snapshotHandle src-volume-path is `pvc-5cf0ed9b-6b58-4313-8442-8df57bed6229` for version 2 and `pvc-f6cd98ac-e1f0-4911-888d-931889dff379/pvc-f6cd98ac-e1f0-4911-888d-931889dff379-data` for version 1

#### Volume Handle for shallow copy volumes 

version 1 VolumeHandle: `0;3;'clusterID';'fsuid';'independent-fileset-name';'volume-name';'Complete Path = /ibm/fs0/ind_fileset/.snapshots/snapshot-name/pvc-name/pvc-name-data`

version 2 VolumeHandle: `1;3;'clusterID';'fsuid';'CG Name';'volume-name';'Complete Path' = /ibm/fs0/xxx-ns/.snapshots/snapshot-name/pvc-name`

## DeleteVolume

1. Delete directory Create another directory under 'independent-fileset-name'/'snapshot-name'/'pvc-name' `#BookKeeping` `#ReferenceCount`
snapshot name needs to be derived from path given in VolumeHandle
2. Delete 'independent-fileset-name'/'snapshot-name' if empty
3. Delete the snapshot 'snapshot-name' if there is no snapshot or volume-name directory. This must be already there
4. Delete the independent fileset if there are no snapshot of dependent fileset. This must be already there

## CreateSnapshot

Not supported

## SnapshotDelete

This is for source snapshot delete
1. Check if independent-fileset/snapshot-name directory exist and empty
2. If empty then delete snapshot else return error saying reference exists

## ControllerExpandVolume

Not supported

## VolumeClone

Can be supported. Return not supported for now

## Subdir/fsGroup/SELinux

Will fail if dir does not exist since snapshot is readOnly

## NodePublishVolume/NodeUnpublishVolume

Bind mount snapshot path in volumeHandle to kubelet path. This should not require any code changes

## NodeGetVolumeStats

Not supported.


## Flow - Version 2

**1. Create StorageClass**

```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ibm-spectrum-scale-csi-advance
parameters:
  version: "2"
  volBackendFs: fs0
provisioner: spectrumscale.csi.ibm.com
reclaimPolicy: Delete
volumeBindingMode: Immediate
allowVolumeExpansion: true
```

**2. Create PVC**

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: scale-advance-pvc1
spec:
  accessModes:
  - ReadWriteMany
  resources:
    requests:
      storage: 1Gi
  storageClassName: ibm-spectrum-scale-csi-advance
```

**3. PV will be created for above PVC**

 ```
Name:            pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf
Labels:          <none>
Annotations:     pv.kubernetes.io/provisioned-by: spectrumscale.csi.ibm.com
                 volume.kubernetes.io/provisioner-deletion-secret-name:
                 volume.kubernetes.io/provisioner-deletion-secret-namespace:
Finalizers:      [kubernetes.io/pv-protection]
StorageClass:    ibm-spectrum-scale-csi-advance-1
Status:          Bound
Claim:           testdr/scale-advance-pvc1
Reclaim Policy:  Delete
Access Modes:    RWX
VolumeMode:      Filesystem
Capacity:        1Gi
Node Affinity:   <none>
Message:
Source:
    Type:              CSI (a Container Storage Interface (CSI) volume source)
    Driver:            spectrumscale.csi.ibm.com
    FSType:            gpfs
    VolumeHandle:      1;1;4033149527292681937;5D3D0B0A:64509FB6;4c6db64a-32ea-4c7a-9768-a387539af470-testdr;pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf;/ibm/fs0/4c6db64a-32ea-4c7a-9768-a387539af470-testdr/pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf
    ReadOnly:          false
    VolumeAttributes:      csi.storage.k8s.io/pv/name=pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf
                           csi.storage.k8s.io/pvc/name=scale-advance-pvc1
                           csi.storage.k8s.io/pvc/namespace=testdr
                           storage.kubernetes.io/csiProvisionerIdentity=1692701621844-8081-spectrumscale.csi.ibm.com
                           version=2
                           volBackendFs=fs0
Events:                <none>
```

**4. Create SnapshotClass**

```
apiVersion: snapshot.storage.k8s.io/v1
deletionPolicy: Delete
driver: spectrumscale.csi.ibm.com
kind: VolumeSnapshotClass
metadata:
  name: ibm-spectrum-scale-snapshotclass
parameters:
  snapWindow: "30"
  ```

**5. Create VolumeSnaphost**

```
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: scale-advance-pvc1-snapshot
spec:
  volumeSnapshotClassName: ibm-spectrum-scale-snapshotclass-advance
  source:
    persistentVolumeClaimName: scale-advance-pvc1
```

**6. VolumeSnapshotContent will be created for above VolumeSnapshot**

```
Name:         snapcontent-139520c1-7e65-47be-b793-58113c0ddd19
Namespace:
Labels:       <none>
Annotations:  <none>
API Version:  snapshot.storage.k8s.io/v1
Kind:         VolumeSnapshotContent
Metadata:
  Creation Timestamp:  2023-09-05T09:48:55Z
  Finalizers:
    snapshot.storage.kubernetes.io/volumesnapshotcontent-bound-protection
  Generation:        1
  Resource Version:  28693947
  UID:               d8e6f445-56b1-4f57-a094-ec5388e4b0b7
Spec:
  Deletion Policy:  Delete
  Driver:           spectrumscale.csi.ibm.com
  Source:
    Volume Handle:             1;1;4033149527292681937;5D3D0B0A:64509FB6;4c6db64a-32ea-4c7a-9768-a387539af470-testdr;pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf;/ibm/fs0/4c6db64a-32ea-4c7a-9768-a387539af470-testdr/pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf
  Volume Snapshot Class Name:  ibm-spectrum-scale-snapshotclass-advance
  Volume Snapshot Ref:
    API Version:       snapshot.storage.k8s.io/v1
    Kind:              VolumeSnapshot
    Name:              scale-advance-pvc1-snapshot
    Namespace:         testdr
    Resource Version:  28693893
    UID:               139520c1-7e65-47be-b793-58113c0ddd19
Status:
  Creation Time:    1693907338000000000
  Ready To Use:     true
  Restore Size:     1073741824
  Snapshot Handle:  1;1;4033149527292681937;5D3D0B0A:64509FB6;4c6db64a-32ea-4c7a-9768-a387539af470-testdr;pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf;snapshot-139520c1-7e65-47be-b793-58113c0ddd19;snapshot-5a4e6bdf-5db0-4c02-9f5e-d0b937f578dc
Events:             <none>
``` 

**7. Create PVC from Snapshot**

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ibm-spectrum-scale-pvc-advance-from-snapshot
spec:
  accessModes:
  - ReadOnlyMany
  resources:
    requests:
      storage: 1Gi
  storageClassName: ibm-spectrum-scale-csi-advance
  dataSource:
    name: scale-advance-pvc1-snapshot
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io

```

**8. Shallow copy PV will be created for above PVC**

```
# oc describe pv pvc-3fe89152-b707-449c-95e2-c056745f7ee8
Name:            pvc-3fe89152-b707-449c-95e2-c056745f7ee8
Labels:          <none>
Annotations:     pv.kubernetes.io/provisioned-by: spectrumscale.csi.ibm.com
                 volume.kubernetes.io/provisioner-deletion-secret-name:
                 volume.kubernetes.io/provisioner-deletion-secret-namespace:
Finalizers:      [kubernetes.io/pv-protection external-attacher/spectrumscale-csi-ibm-com]
StorageClass:    ibm-spectrum-scale-csi-fileset
Status:          Bound
Claim:           testdr/ibm-spectrum-scale-pvc-advance-from-snapshot
Reclaim Policy:  Delete
Access Modes:    ROX
VolumeMode:      Filesystem
Capacity:        1Gi
Node Affinity:   <none>
Message:
Source:
    Type:              CSI (a Container Storage Interface (CSI) volume source)
    Driver:            spectrumscale.csi.ibm.com
    FSType:            gpfs
    VolumeHandle:      1;3;4033149527292681937;5D3D0B0A:64509FB6;4c6db64a-32ea-4c7a-9768-a387539af470-testdr;pvc-3fe89152-b707-449c-95e2-c056745f7ee8;/ibm/fs0/4c6db64a-32ea-4c7a-9768-a387539af470-testdr/.snapshots/snapshot-139520c1-7e65-47be-b793-58113c0ddd19/pvc-bb945776-395a-4e63-8d3e-d1f6e826cdbf
    ReadOnly:          false
    VolumeAttributes:      csi.storage.k8s.io/pv/name=pvc-3fe89152-b707-449c-95e2-c056745f7ee8
                           csi.storage.k8s.io/pvc/namespace=testdr
                           volBackendFs=fs0
                           version=2

 ```

## Flow - Version 1

**1. Create StorageClass**

```
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: ibm-spectrum-scale-csi-fileset
parameters:
  volBackendFs: fs0
provisioner: spectrumscale.csi.ibm.com
reclaimPolicy: Delete
volumeBindingMode: Immediate
```

**2. Create PVC**

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: scale-fset-pvc
spec:
  accessModes:
  - ReadWriteMany
  resources:
    requests:
      storage: 1Gi
  storageClassName: ibm-spectrum-scale-csi-fileset
```

**3. PV will be created for above PVC**

```

# oc describe pv pvc-f6cd98ac-e1f0-4911-888d-931889dff379
Name:            pvc-f6cd98ac-e1f0-4911-888d-931889dff379
Labels:          <none>
Annotations:     pv.kubernetes.io/provisioned-by: spectrumscale.csi.ibm.com
                 volume.kubernetes.io/provisioner-deletion-secret-name:
                 volume.kubernetes.io/provisioner-deletion-secret-namespace:
Finalizers:      [kubernetes.io/pv-protection external-attacher/spectrumscale-csi-ibm-com]
StorageClass:    ibm-spectrum-scale-csi-fileset
Status:          Bound
Claim:           default/scale-fset-pvc
Reclaim Policy:  Delete
Access Modes:    RWX
VolumeMode:      Filesystem
Capacity:        1Gi
Node Affinity:   <none>
Message:
Source:
    Type:              CSI (a Container Storage Interface (CSI) volume source)
    Driver:            spectrumscale.csi.ibm.com
    FSType:            gpfs
    VolumeHandle:      0;2;4033149527292681937;5D3D0B0A:64509FB6;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379;/ibm/fs0/spectrum-scale-csi-volume-store/.volumes/pvc-f6cd98ac-e1f0-4911-888d-931889dff379
    ReadOnly:          false
    VolumeAttributes:      csi.storage.k8s.io/pv/name=pvc-f6cd98ac-e1f0-4911-888d-931889dff379
                           csi.storage.k8s.io/pvc/name=scale-fset-pvc
                           csi.storage.k8s.io/pvc/namespace=default
                           storage.kubernetes.io/csiProvisionerIdentity=1693120059860-8081-spectrumscale.csi.ibm.com
                           volBackendFs=fs0
```


**4. Create SnapshotClass**

```
apiVersion: snapshot.storage.k8s.io/v1
deletionPolicy: Delete
driver: spectrumscale.csi.ibm.com
kind: VolumeSnapshotClass
metadata:
  name: ibm-spectrum-scale-snapshotclass
parameters:
  snapWindow: "30"
  ```

**5. Create VolumeSnapshot**

```
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: scale-fset-pvc-snapshot
spec:
  volumeSnapshotClassName: ibm-spectrum-scale-snapshotclass
  source:
    persistentVolumeClaimName: scale-fset-pvc
```


**6. VolumeSnapshotContent will be created for above VolumeSnapshot**
   
```
Name:         snapcontent-b4f6236f-01e4-4c67-9bf5-39ec0969c9f9
Namespace:
Labels:       <none>
Annotations:  <none>
API Version:  snapshot.storage.k8s.io/v1
Kind:         VolumeSnapshotContent
Metadata:
  Creation Timestamp:  2023-08-30T05:18:19Z
  Finalizers:
    snapshot.storage.kubernetes.io/volumesnapshotcontent-bound-protection
  Generation:        1
  Resource Version:  27309307
  UID:               031f9c0a-ee2e-41b2-af45-0fad9bda02f7
Spec:
  Deletion Policy:  Delete
  Driver:           spectrumscale.csi.ibm.com
  Source:
    Volume Handle:             0;2;4033149527292681937;5D3D0B0A:64509FB6;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379;/ibm/fs0/spectrum-scale-csi-volume-store/.volumes/pvc-f6cd98ac-e1f0-4911-888d-931889dff379
  Volume Snapshot Class Name:  ibm-spectrum-scale-snapshotclass-advance
  Volume Snapshot Ref:
    API Version:       snapshot.storage.k8s.io/v1
    Kind:              VolumeSnapshot
    Name:              scale-fset-pvc-snapshot
    Namespace:         default
    Resource Version:  27309288
    UID:               b4f6236f-01e4-4c67-9bf5-39ec0969c9f9
Status:
  Creation Time:    1693372700000000000
  Ready To Use:     true
  Restore Size:     1073741824
  Snapshot Handle:  0;2;4033149527292681937;5D3D0B0A:64509FB6;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379;snapshot-b4f6236f-01e4-4c67-9bf5-39ec0969c9f9;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379-data
```

**7. Create PVC from Snapshot**

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ibm-spectrum-scale-pvc-from-snapshot
spec:
  accessModes:
  - ReadOnlyMany
  resources:
    requests:
      storage: 1Gi
  storageClassName: ibm-spectrum-scale-csi-fileset
  dataSource:
    name: scale-fset-pvc-snapshot
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io

```

**8. PV will be created for above PVC**


```
# oc describe pv pvc-1b7a5134-e6b0-437a-8b40-4977f1298616
Name:            pvc-1b7a5134-e6b0-437a-8b40-4977f1298616
Labels:          <none>
Annotations:     pv.kubernetes.io/provisioned-by: spectrumscale.csi.ibm.com
                 volume.kubernetes.io/provisioner-deletion-secret-name:
                 volume.kubernetes.io/provisioner-deletion-secret-namespace:
Finalizers:      [kubernetes.io/pv-protection external-attacher/spectrumscale-csi-ibm-com]
StorageClass:    ibm-spectrum-scale-csi-fileset
Status:          Bound
Claim:           default/ibm-spectrum-scale-pvc-from-snapshot
Reclaim Policy:  Delete
Access Modes:    ROX
VolumeMode:      Filesystem
Capacity:        1Gi
Node Affinity:   <none>
Message:
Source:
    Type:              CSI (a Container Storage Interface (CSI) volume source)
    Driver:            spectrumscale.csi.ibm.com
    FSType:            gpfs
    VolumeHandle:      0;3;4033149527292681937;5D3D0B0A:64509FB6;pvc-f6cd98ac-e1f0-4911-888d-931889dff379;pvc-1b7a5134-e6b0-437a-8b40-4977f1298616;/ibm/fs0/pvc-f6cd98ac-e1f0-4911-888d-931889dff379/.snapshots/snapshot-b4f6236f-01e4-4c67-9bf5-39ec0969c9f9/pvc-f6cd98ac-e1f0-4911-888d-931889dff379/pvc-f6cd98ac-e1f0-4911-888d-931889dff379-data
    ReadOnly:          false
    VolumeAttributes:      csi.storage.k8s.io/pv/name=pvc-1b7a5134-e6b0-437a-8b40-4977f1298616
                           csi.storage.k8s.io/pvc/namespace=default
                           volBackendFs=fs0
 ```






