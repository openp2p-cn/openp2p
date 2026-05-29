#include <iostream>
#include <windows.h>

using namespace std;
typedef void (*pRun)(const char *);

int main(int argc, char *argv[])
{
	HMODULE dll = LoadLibraryA("openp2p.dll");
	pRun run = (pRun)GetProcAddress(dll, "RunCmd");
	run("-node 5800-debug2 -token YOUR-TOKEN");
	FreeLibrary(dll);
	return 0;
}
