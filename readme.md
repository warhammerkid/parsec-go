# Parsec API

## POST /api/RequestRaidGroup
Creates a raid group with the given group name, password, and admin password. The
admin password is needed to delete the group, should that ever be desired. **NOTE:
The passwords are currently not being hashed in the database, so do not use a
password you use anywhere else**

+ Request (application/json)

        {
          "requestedName": "RAID GROUP NAME",
          "requestedPassword": "PASSWORD",
          "adminPassword": "ADMIN PASSWORD"
        }

+ Response 200 (application/json)

        {
          "Success": true,
          "Message": "Raid group created successfully"
        }

+ Response 200 (application/json)

        {
          "Success": false,
          "Message": "A group with the given name already exists"
        }

## POST /api/DeleteRaidGroup
Deletes the raid group with the given name if the admin password matches what was
used to create the group.

+ Request (application/json)

        {
          "groupName": "RAID GROUP NAME",
          "adminPassword": "ADMIN PASSWORD"
        }

+ Response 200 (application/json)

        {
          "Success": true,
          "Message": "Raid group deleted successfully"
        }

+ Response 200 (application/json)

        {
          "Success": false,
          "Message": "An unknown error was encountered"
        }

## POST /api/TestConnection
Verify that you can connect to the service with the given credentials.

+ Request (application/json)

        {
          "RaidGroup": "RAID GROUP NAME",
          "RaidPassword": "PASSWORD"
        }

+ Response 200 (application/json)

        {
          "ErrorMessage": ""
        }

+ Response 200 (application/json)

        {
          "ErrorMessage": "Connection failed"
        }

## POST /api/GetRaidStats
Returns a list of all user stats for the raid group

+ Request (application/json)

        {
          "RaidGroup": "RAID GROUP NAME",
          "RaidPassword": "PASSWORD"
        }

+ Response 200 (application/json)

        {
          "ErrorMessage": ""
          "Users": [
            {
              "RaidUserId":5,
              "RaidGroupId":5001,
              "LastConnectDate":"2014-05-17T23:39:20Z",
              "IsConnected":true,
              "CharacterName":"Karmeld",
              "DamageOut":2000,
              "DamageIn":100,
              "HealOut":0,
              "EffectiveHealOut":0,
              "HealIn":0,
              "Threat":0,
              "RaidEncounterId":0,
              "RaidEncounterMode":0,
              "RaidEncounterPlayers":0,
              "CombatTicks":0,
              "CombatStart":"",
              "CombatEnd":"",
              "LastCombatUpdate":"2014-05-17T23:39:20Z"
            },
            ...
          ],
          MinimumPollingRate": 1
        }

## POST /api/SyncRaidStats
Updates the raid stats with the given user's stats and returns the stats of all
users in the raid group.

+ Request (application/json)

        {
          "RaidGroup": "RAID GROUP NAME",
          "RaidPassword": "PASSWORD",
          "Statistics": {
              "RaidUserId":5,
              "CharacterName":"Karmeld",
              "DamageOut":2000,
              "DamageIn":100,
              "HealOut":0,
              "EffectiveHealOut":0,
              "HealIn":0,
              "Threat":0,
              "RaidEncounterId":0,
              "RaidEncounterMode":0,
              "RaidEncounterPlayers":0,
              "CombatTicks":0,
              "CombatStart":"",
              "CombatEnd":""
          }
        }

+ Response 200 (application/json)

        {
          "ErrorMessage": ""
          "Users": [
            {
              "RaidUserId":5,
              "RaidGroupId":5001,
              "LastConnectDate":"2014-05-17T23:39:20Z",
              "IsConnected":true,
              "CharacterName":"Karmeld",
              "DamageOut":2000,
              "DamageIn":100,
              "HealOut":0,
              "EffectiveHealOut":0,
              "HealIn":0,
              "Threat":0,
              "RaidEncounterId":0,
              "RaidEncounterMode":0,
              "RaidEncounterPlayers":0,
              "CombatTicks":0,
              "CombatStart":"",
              "CombatEnd":"",
              "LastCombatUpdate":"2014-05-17T23:39:20Z"
            },
            ...
          ],
          MinimumPollingRate": 1
        }