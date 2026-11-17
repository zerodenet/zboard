package datastore

import "gorm.io/gorm"

func ReconcileNodeObservationForTest(db *gorm.DB) error { return reconcileNodeObservationSchema(db) }
