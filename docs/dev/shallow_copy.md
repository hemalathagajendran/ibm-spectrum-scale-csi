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

### Controller operations

Care must be taken when handling life-times of relevant storage resources. When
a shallow volume is created, what would happen if:

* _Parent volume of the snapshot is removed while the shallow volume still
  exists?_

  Deletion of volume will fails since snapshot exist for it

* _Source snapshot from which the shallow volume originates is removed while
  that shallow volume still exists?_

  We need to make sure this doesn't happen and some book-keeping is necessary.

#### Book-keeping for shallow volumes

As mentioned above, this is to protect shallow volumes, should their source
snapshot be requested for deletion.

VolumeCreation : Reference count will be added to Snapshot

SnapshotDeletion : Delete snapshot if no reference to shallow volume otherwise
Fail with error saying shallow volume exist.

VolumeDeletion : Reference count will be removed from Snapshot

#### `CreateVolume`

A read-only volume with snapshot source would be created under these conditions:

1. `CreateVolumeRequest.volume_content_source` is a snapshot,
2. `CreateVolumeRequest.volume_capabilities[*].access_mode` is any of read-only
   volume access modes.

Things to look out for:

* _What's the volume size?_

  It can be Zero or anything as this is not actually consuming any storage but
  we have to keep cloning in mind for size

### `DeleteVolume`

Update the snapshot reference.

### `CreateSnapshot`

Not supported

### `ControllerExpandVolume`

Not supported

### `VolumeClone`

Can be supported. Return not supported for now.

### `Subdir/fsGroup/SELinux`

Will fail if dir does not exist since snapshot is readOnly

### `NodePublishVolume`, `NodeUnpublishVolume`

Bind mount snapshot path in volumeHandle to kubelet path. This should not require any code changes

### `NodeGetVolumeStats`

Not supported.

### `Volume Handle`

version-1 VolumeHandle: 0;3;'clusterID';'fsuid';'independent-fileset-name';'volume-name';'Complete Path = /ibm/fs0/ind_fileset/.snapshots/snapshot-name/pvc-name/pvc-name-data
version-2 VolumeHandle: 1;3;'clusterID';'fsuid';'CG Name';'volume-name';'Complete Path' = /ibm/fs0/xxx-ns/.snapshots/snapshot-name/pvc-name

### Book Keeping

## VolumeCreate

1. Check if snapshot exist. Snapshot details will be in createVolume request. We need snapshot(independent fileset / CG fileset) and not the snapshot representing pvc which needs be extracted from create volume request

for example -

a. Version 1 snapshot handle will look like following. we need snapshot name = snapshot-b4f6236f-01e4-4c67-9bf5-39ec0969c9f9
0;2;4033149527292681937;5D3D0B0A:64509FB6;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379;snapshot-b4f6236f-01e4-4c67-9bf5-39ec0969c9f9;;pvc-f6cd98ac-e1f0-4911-888d-931889dff379-data
b. Version 2 snapshot handle will look like following. we need snapshot name = snapshot-b515a1c2-9fe6-47df-9a86-10a20b7965c6
1;1;4033149527292681937;5D3D0B0A:64509FB6;4c6db64a-32ea-4c7a-9768-a387539af470-default;pvc-5cf0ed9b-6b58-4313-8442-8df57bed6229;snapshot-b515a1c2-9fe6-47df-9a86-10a20b7965c6;snapshot-b515a1c2-9fe6-47df-9a86-10a20b7965c6
2. Create directory under "independent-fileset-name" with the name 'snapshot name'
3. Create another directory under 'independent-fileset-name'/'snapshot-name'/'volume-name'
4. Return volume handle with path /ibm/fs1/'independent-fileset-name'/.snapshots/'snapshot-name'/'src-volume-path'
From above snapshotHandle src-volume-path is pvc-5cf0ed9b-6b58-4313-8442-8df57bed6229 for version 2 and pvc-f6cd98ac-e1f0-4911-888d-931889dff379/pvc-f6cd98ac-e1f0-4911-888d-931889dff379-data for version 1

## VolumeDelete

1. Delete directory Create another directory under 'independent-fileset-name'/'snapshot-name'/'pvc-name'
snapshot name needs to be derived from path given in VolumeHandle
2. Delete 'independent-fileset-name'/'snapshot-name' if empty
3. Delete the snapshot 'snapshot-name' if there is no snapshot or volume-name directory. This must be already there
Note: We must have mutex to create and delete directory
4. Delete the independent fileset if there are no snapshot of dependent fileset. This must be already there

## Snapshot Delete

1. Check if independent-fileset/snapshot-name directory exist and empty
2. If empty then delete snapshot else return error saying reference exists

## StorageClass

Ideally user will create with with AccessMode=ReadOnlyMany with same storageClass as of source pvc and if there is different then it might not cause issue but to keep it simple and alined we will mandate following

1. Version must be same
2. VolumeBackend must be same
3. FilesetType must be same in case of version1
