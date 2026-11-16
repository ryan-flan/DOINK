package toon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const doinkLua = `DOINKDB = {
["version"] = 1,
["webhooks"] = {
["*"] = "https://discord.com/api/webhooks/1/x",
},
["chars"] = {
["Flano Wren-Whatever"] = {
["config"] = {
},
["events"] = {
"{\"char\":\"Flano\",\"surname\":\"Wren\",\"realm\":\"Whatever\",\"class\":\"WARRIOR\",\"level\":20,\"seq\":1,\"ts\":1000,\"type\":\"level_up\",\"data\":{\"level\":20}}",
"{\"char\":\"Flano\",\"surname\":\"Wren\",\"realm\":\"Whatever\",\"class\":\"WARRIOR\",\"level\":20,\"seq\":2,\"ts\":1100,\"type\":\"death\",\"played\":5000,\"data\":{\"zone\":\"Westfall\",\"subzone\":null,\"killer\":\"Defias Pillager\",\"environment\":null}}",
"{\"char\":\"Flano\",\"surname\":\"Wren\",\"realm\":\"Whatever\",\"class\":\"WARRIOR\",\"level\":20,\"seq\":3,\"ts\":1200,\"type\":\"quest\",\"test\":true,\"data\":{\"quest_id\":1,\"title\":\"Test\",\"xp\":1}}",
},
["seq"] = 3,
},
["Myra Nut-Whatever"] = {
["events"] = {
"{\"char\":\"Myra\",\"surname\":\"Nut\",\"realm\":\"Whatever\",\"class\":\"WARLOCK\",\"level\":24,\"seq\":1,\"ts\":900,\"type\":\"boss_kill\",\"data\":{\"name\":\"Cookie\",\"instance\":\"Deadmines\",\"encounter_id\":2746,\"difficulty\":1,\"success\":true,\"group_size\":5,\"first_kill\":true,\"group\":null}}",
},
["seq"] = 1,
},
["Empty One-Whatever"] = {
["config"] = {
},
},
},
}
`

func touch(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestRead(t *testing.T) {
	account := filepath.Join(t.TempDir(), "_forever_", "WTF", "Account", "1#1")
	sv := filepath.Join(account, "SavedVariables", "DOINK.lua")
	touch(t, sv, time.Unix(5000, 0))
	if err := os.WriteFile(sv, []byte(doinkLua), 0o644); err != nil {
		t.Fatal(err)
	}
	// Myra logged in last, though Flano has the newer events; Coco has
	// a folder and nothing in DOINK.
	touch(t, filepath.Join(account, "91", "Myra-Nut", "cache.md5"), time.Unix(3000, 0))
	touch(t, filepath.Join(account, "91", "Flano-Wren", "cache.md5"), time.Unix(2000, 0))
	touch(t, filepath.Join(account, "91", "Crappy-Coco", "AddOns.txt"), time.Unix(1500, 0))

	s := Read([]string{sv, filepath.Join(t.TempDir(), "missing.lua")})
	if s.Last == nil || s.Last.FullName() != "Myra Nut" {
		t.Fatalf("last = %+v", s.Last)
	}
	if s.Last.Class != "WARLOCK" || s.Last.Level != 24 || s.Last.LastSeen != 3000 || s.Last.Realm != "Whatever" {
		t.Errorf("myra = %+v", s.Last)
	}
	if len(s.Last.Recent) != 1 || s.Last.Recent[0].Text != "Killed Cookie (Deadmines)" {
		t.Errorf("myra recent = %+v", s.Last.Recent)
	}
	if len(s.Others) != 3 {
		t.Fatalf("others = %+v", s.Others)
	}
	flano := s.Others[0]
	if flano.FullName() != "Flano Wren" || flano.Played != 5000 || flano.LastSeen != 2000 {
		t.Errorf("flano = %+v", flano)
	}
	// The test event is left out; newest first.
	if flano.Counts["quest"] != 0 || flano.Counts["death"] != 1 || len(flano.Recent) != 2 ||
		flano.Recent[0].Text != "Died in Westfall (Defias Pillager)" {
		t.Errorf("flano events = %+v %+v", flano.Counts, flano.Recent)
	}
	if s.Others[1].FullName() != "Crappy Coco" || s.Others[1].LastSeen != 1500 {
		t.Errorf("coco = %+v", s.Others[1])
	}
	if s.Others[2].FullName() != "Empty One" || s.Others[2].LastSeen != 0 || s.Others[2].Recent == nil {
		t.Errorf("empty = %+v", s.Others[2])
	}
}

func TestReadNothing(t *testing.T) {
	s := Read(nil)
	if s.Last != nil || s.Others == nil || len(s.Others) != 0 {
		t.Errorf("got %+v", s)
	}
}
