// Command client is the GophKeeper CLI.
package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/kmorozov/gophkeeper/internal/buildinfo"
	"github.com/kmorozov/gophkeeper/internal/client"
	pb "github.com/kmorozov/gophkeeper/proto/gophkeeper/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var serverAddr string

func main() {
	cfg := client.NewConfig()

	root := &cobra.Command{
		Use:   "gophkeeper",
		Short: "GophKeeper is a password and secrets manager",
	}
	root.PersistentFlags().StringVarP(&serverAddr, "address", "a", cfg.Addr,
		"gRPC server address (or GOPHKEEPER_ADDR)")
	root.AddCommand(
		registerCmd(),
		loginCmd(),
		pingCmd(),
		versionCmd(),
		itemsCmd(),
	)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// newClient builds a connected client for the configured server.
func newClient() (*client.GophKeeperClient, func()) {
	c, closeFn := newClientWithConfig()
	return c, closeFn
}

// newCachedClient builds a client with the local SQLite cache attached.
func newCachedClient() (*client.CachedClient, func()) {
	base, closeBase := newClientWithConfig()
	c, err := client.NewCachedClient(base, client.NewConfig().CachePath)
	if err != nil {
		closeBase()
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return c, func() {
		_ = c.Close()
		closeBase()
	}
}

// newClientWithConfig builds the gRPC client honoring the address flag.
func newClientWithConfig() (*client.GophKeeperClient, func()) {
	cfg := client.NewConfig()
	if serverAddr != "" {
		cfg.Addr = serverAddr
	}
	c, err := client.NewGophKeeperClient(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	return c, func() { _ = c.Close() }
}

// registerCmd builds the register subcommand.
func registerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "register <login> <password>",
		Short: "Create a new user",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			c, closeFn := newClient()
			defer closeFn()
			resp, err := c.Register(cmd.Context(), args[0], args[1])
			if err != nil {
				fail(err)
			}
			if err := c.Tokens().Save(client.TokenRecord{
				Token:     resp.GetToken(),
				UserID:    resp.GetUserId(),
				Login:     args[0],
				UpdatedAt: time.Now(),
			}); err != nil {
				fail(err)
			}
			fmt.Println("registered, token saved")
		},
	}
}

// loginCmd builds the login subcommand.
func loginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login <login> <password>",
		Short: "Log in and store the token",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			c, closeFn := newClient()
			defer closeFn()
			resp, err := c.Login(cmd.Context(), args[0], args[1])
			if err != nil {
				fail(err)
			}
			if err := c.Tokens().Save(client.TokenRecord{
				Token:     resp.GetToken(),
				UserID:    resp.GetUserId(),
				Login:     args[0],
				UpdatedAt: time.Now(),
			}); err != nil {
				fail(err)
			}
			fmt.Println("logged in, token saved")
		},
	}
}

// pingCmd builds the ping subcommand.
func pingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Check connectivity and token validity",
		Run: func(cmd *cobra.Command, args []string) {
			c, closeFn := newClient()
			defer closeFn()
			resp, err := c.Ping(cmd.Context())
			if err != nil {
				fail(err)
			}
			fmt.Println("ok:", resp.GetOk())
		},
	}
}

// versionCmd builds the version subcommand.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(buildinfo.String())
		},
	}
}

// itemsCmd builds the items command group.
func itemsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "items",
		Short: "Manage stored secrets",
	}
	cmd.AddCommand(itemsListCmd(), itemsGetCmd(), itemsCreateCmd(), itemsDeleteCmd())
	return cmd
}

// itemsListCmd builds the items list subcommand.
func itemsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all secrets",
		Run: func(cmd *cobra.Command, args []string) {
			c, closeFn := newCachedClient()
			defer closeFn()
			items, fromCache, err := c.ListItems(cmd.Context())
			if err != nil {
				fail(err)
			}
			if fromCache {
				fmt.Fprintln(os.Stderr, "warning: server unavailable, showing cached data")
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTYPE\tNAME\tUPDATED")
			for _, item := range items {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					item.GetId(), item.GetType(), item.GetName(), formatTime(item.GetUpdatedAt()))
			}
			w.Flush()
		},
	}
}

// itemsGetCmd builds the items get subcommand.
func itemsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Show a single secret",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			c, closeFn := newCachedClient()
			defer closeFn()
			item, fromCache, err := c.GetItem(cmd.Context(), args[0])
			if err != nil {
				fail(err)
			}
			if fromCache {
				fmt.Fprintln(os.Stderr, "warning: server unavailable, showing cached data")
			}
			fmt.Printf("id: %s\ntype: %s\nname: %s\nlogin: %s\npassword: %s\ncard number: %s\ncard exp: %s\ncard cvv: %s\nmeta: %s\ndata: %s\nupdated: %s\n",
				item.GetId(), item.GetType(), item.GetName(), item.GetLogin(), item.GetPassword(),
				item.GetCardNumber(), item.GetCardExp(), item.GetCardCvv(), item.GetMeta(),
				string(item.GetData()), formatTime(item.GetUpdatedAt()))
		},
	}
}

// itemsDeleteCmd builds the items delete subcommand.
func itemsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a secret",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			c, closeFn := newCachedClient()
			defer closeFn()
			if err := c.DeleteItem(cmd.Context(), args[0]); err != nil {
				fail(err)
			}
			fmt.Println("deleted")
		},
	}
}

// itemsCreateCmd builds the items create subcommand.
func itemsCreateCmd() *cobra.Command {
	var itemType, name, login, password, data, cardNumber, cardExp, cardCVV, meta string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a secret",
		Run: func(cmd *cobra.Command, args []string) {
			if cmd.Flags().NFlag() == 0 {
				cmd.Help()
				os.Exit(1)
			}
			kind, ok := pb.ItemType_value[strings.ToUpper(itemType)]
			if !ok {
				fmt.Fprintf(os.Stderr, "error: unknown item type %q (LOGIN, TEXT, BINARY, CARD)\n", itemType)
				os.Exit(1)
			}
			c, closeFn := newCachedClient()
			defer closeFn()
			item := &pb.Item{
				Type:       pb.ItemType(kind),
				Name:       name,
				Login:      login,
				Password:   password,
				CardNumber: cardNumber,
				CardExp:    cardExp,
				CardCvv:    cardCVV,
				Meta:       meta,
			}
			if data != "" {
				var err error
				item.Data, err = readData(data)
				if err != nil {
					fail(err)
				}
			}
			id, err := c.CreateItem(cmd.Context(), item)
			if err != nil {
				fail(err)
			}
			fmt.Println("created:", id)
		},
	}
	cmd.Flags().StringVar(&itemType, "type", "LOGIN", "item type: LOGIN, TEXT, BINARY, CARD")
	cmd.Flags().StringVar(&name, "name", "", "item name")
	cmd.Flags().StringVar(&login, "login", "", "stored login")
	cmd.Flags().StringVar(&password, "password", "", "stored password")
	cmd.Flags().StringVar(&data, "data", "", "data payload, use @file to read a file")
	cmd.Flags().StringVar(&cardNumber, "card-number", "", "card number")
	cmd.Flags().StringVar(&cardExp, "card-exp", "", "card expiration date")
	cmd.Flags().StringVar(&cardCVV, "card-cvv", "", "card CVV")
	cmd.Flags().StringVar(&meta, "meta", "", "free-form metadata")
	return cmd
}

// readData reads the data flag value, supporting the @file syntax.
func readData(data string) ([]byte, error) {
	if strings.HasPrefix(data, "@") {
		return os.ReadFile(strings.TrimPrefix(data, "@"))
	}
	return []byte(data), nil
}

// formatTime renders a protobuf timestamp for output.
func formatTime(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().Format(time.RFC3339)
}

// fail prints an error and exits with a non-zero code.
func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
