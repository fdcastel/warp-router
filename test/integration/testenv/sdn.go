package testenv

// SDN zone provisioning is handled by PVE.CreateBridge / PVE.DestroyBridge
// in pve.go. This file is kept for backward compatibility but no longer
// contains active provisioning logic.
//
// Test network isolation is achieved via ephemeral Linux bridges created
// per test run (named wt<runID>), not Proxmox SDN zones.
