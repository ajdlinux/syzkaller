package main

import (
	"github.com/google/syzkaller/vm/vmimpl"
	"github.com/google/syzkaller/vm/openstack"

	"fmt"
	"time"
)

func main() {
	fmt.Println("OpenStack test tool")

	config := []byte(`{
"count": 1,
"flavor": "tiny",
"image": "Ubuntu_1804_VM_Image-Gold",
"cloud": {
"verify": false,
"auth": {
"auth_url": "https://powervc.ozlabs.ibm.com:5000/",
"username": "root",
"password": "ltc0zlabs",
"user_domain_id": "default",
"project_name": "ibm-default",
"project_domain_id": "default"
}
}
}`)
	
	env := vmimpl.Env{
		Name: "syztest",
		Config: config,
	}

	pool, err := openstack.TestCtor(&env)
	if err != nil {
		panic(err)
	}

	inst, err := pool.Create(".", 1)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", inst)
	out, errc, err := inst.Run(time.Minute, nil, "ls")
	
	fmt.Printf("%+v\n%+v\n", out, errc)
}
