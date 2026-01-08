# Protocole WebSocket — SR-S9-Projet-Serveur

But : définir les messages JSON échangés entre clients et serveur pour le jeu "ramasser des bonbons".

## Principes généraux
- Le serveur est **authoritative** : il décide des positions, résout les collisions et attribue les collectes.
- Les clients envoient des *intents* (ex: `move`) ; le serveur applique les commandes lors d'un tick central et envoie des snapshots `state` périodiquement.
- Fréquence par défaut : **10 ticks/s** (modifiable).
- Grille : par défaut **10x10** (coordonnées x,y entières dans [0,9]).

---

## Messages — format général
Chaque message est un objet JSON avec au moins le champ `type` :
```
{ "type": "<string>", ... }
```

### Client → Serveur
- Join
```
{ "type": "join", "name": "Alice" }
```
- Move (intent)
```
{ "type": "move", "dir": "up" }
// dir ∈ {"up","down","left","right"}
```
- Move (option: absolute)
```
{ "type": "move", "x": 3, "y": 5 }
```
- Ping (optionnel, pour latence)
```
{ "type": "ping", "ts": 1670000000 }
```

### Serveur → Client
- Join Ack
```
{ "type":"join_ack", "id":"p-1", "pos":{"x":1,"y":2}, "grid":{"w":10,"h":10} }
```
- State (snapshot complet)
```
{
  "type":"state",
  "tick": 123,
  "players": [ {"id":"p-1","name":"A","x":1,"y":2,"score":3}, ... ],
  "sweets": [ {"id":"s1","x":4,"y":5}, ... ]
}
```
- Event (notification ponctuelle)
```
{ "type":"event","event":"collected","player":"p-1","sweet":"s1","tick":124 }
```
- Error
```
{ "type":"error","message":"unknown command" }
```
- Game Over
```
{ "type":"game_over","scores":[ {"id":"p-1","score":5}, ... ] }
```

---

## Règles & invariants
- Deux joueurs **ne peuvent pas** occuper la même case après résolution d'un tick.
- Une sucrerie (sweet) est supprimée et assignée au premier joueur qui l'occupe durant la résolution d'un tick.
- Si deux joueurs entrent la même case contenant une sucrerie dans le même tick, le serveur résout le conflit selon une règle déterministe (ex : priorité par `id` ou par ordre d'arrivée des messages) — à définir dans l'implémentation.

---

## Séquence d'exemple
1. Client se connecte en WS à `/ws`.
2. Envoie `{ "type":"join","name":"Alice" }`.
3. Reçoit `{ "type":"join_ack", ... }`.
4. À chaque frame de jeu, envoie `move` intent.
5. Le serveur envoie périodiquement `state` ; le client met à jour son rendu.

---

## Tests rapides (exemples)
- Avec `websocat` :
```
# ouvrir une connexion
websocat ws://localhost:8080/ws
# envoyer un join
{"type":"join","name":"Bot1"}
# envoyer un move
{"type":"move","dir":"right"}
```

---

## Extension possible
- Ajouter messages `chat`, `spectate`, `reconnect` (avec token) et autoriser des options de configuration (taille de grille, nombre de sweets).

---

## Validation & erreurs
- Le serveur renvoie un message `error` si le JSON est mal formé ou si un champ requis manque.
- Le client doit accepter que l'état reçu soit la vérité (autoritative server).

---

Fichier créé pour servir de référence pendant l'implémentation du moteur de jeu et des clients.
